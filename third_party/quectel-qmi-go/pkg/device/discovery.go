package device

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/iniwex5/quectel-qmi-go/pkg/manager"
	"github.com/iniwex5/quectel-qmi-go/pkg/qmi"
)

func init() {
	manager.SetDeviceDiscoverer(Discover)
}

// Discover 查找可用于 QMI 的调制解调器（兼容旧行为：默认严格要求 control path）。
func Discover() ([]manager.ModemDevice, error) {
	return discover(true)
}

// DiscoverAll 查找所有可识别的调制解调器（包含非QMI模式设备）。
func DiscoverAll() ([]manager.ModemDevice, error) {
	return discover(false)
}

func discover(requireControlPath bool) ([]manager.ModemDevice, error) {
	var devices []manager.ModemDevice

	usbDevices, err := os.ReadDir("/sys/bus/usb/devices")
	if err != nil {
		return nil, fmt.Errorf("读取 USB 设备失败: %w", err)
	}

	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, entry := range usbDevices {
		if strings.HasPrefix(entry.Name(), "usb") {
			continue
		}

		wg.Add(1)
		go func(e os.DirEntry) {
			defer wg.Done()

			path := filepath.Join("/sys/bus/usb/devices", e.Name())

			// 包装 discoverFromSysFS，增加 5秒 超时保护
			// 这里的超时主要防止 discoverFromSysFS 内部可能有较慢的文件操作。
			type result struct {
				val *manager.ModemDevice
				err error
			}
			done := make(chan result, 1)

			go func() {
				md, err := discoverFromSysFS(path)
				done <- result{md, err}
			}()

			select {
			case res := <-done:
				if res.err == nil && res.val != nil {
					if requireControlPath && strings.TrimSpace(res.val.ControlPath) == "" {
						return
					}
					mu.Lock()
					devices = append(devices, *res.val)
					mu.Unlock()
				}
			case <-time.After(5 * time.Second): // 单个设备最大扫描时间 5s
				// 超时忽略
				// fmt.Printf("扫描设备 %s 超时\n", path)
			}
		}(entry)
	}

	wg.Wait()

	if len(devices) == 0 {
		return nil, fmt.Errorf("未发现调制解调器")
	}

	return devices, nil
}

// discoverFromSysFS 检查单个 USB 设备路径
func discoverFromSysFS(usbPath string) (*manager.ModemDevice, error) {
	scanUSBPath := resolveUSBPath(usbPath)

	// 1. 检查厂商 ID
	vid := readHexFile(filepath.Join(scanUSBPath, "idVendor"))
	pid := readHexFile(filepath.Join(scanUSBPath, "idProduct"))
	// fmt.Printf("Device %s: VID=%04x PID=%04x\n", usbPath, vid, pid)

	if vid != 0x2c7c && vid != 0x05c6 { // Quectel & Qualcomm
		return nil, fmt.Errorf("不是 Quectel 设备")
	}

	// device.c 逻辑: 查找网络接口
	// 它扫描interfaces 0 到 bNumInterfaces+8
	bNumIfaces := readIntFile(filepath.Join(scanUSBPath, "bNumInterfaces"))
	// fmt.Printf("Num interfaces: %d\n", bNumIfaces)

	var netInterface string
	var foundIfaceIndex int

	// 扫描网络接口
	for i := 0; i < bNumIfaces+8; i++ {
		// 尝试路径: usbPath/usbName:1.i/net
		// 上面循环中的 entry.Name() 是 usbName (例如: 1-1)
		// 接口路径: 1-1:1.i
		usbName := filepath.Base(scanUSBPath)
		ifPath := filepath.Join(scanUSBPath, fmt.Sprintf("%s:1.%d", usbName, i))

		netDir := filepath.Join(ifPath, "net")
		entries, err := os.ReadDir(netDir)
		if err == nil && len(entries) > 0 {
			netInterface = entries[0].Name()
			foundIfaceIndex = i
			break
		}
	}

	if netInterface == "" {
		return nil, fmt.Errorf("未找到网络接口")
	}

	md := &manager.ModemDevice{
		USBPath:      usbPath,
		VendorID:     vid,
		ProductID:    pid,
		NetInterface: netInterface,
	}

	// device.c 根据接口类/子类确定驱动类型
	// qmidevice_detect 循环查询 usb_interface_info
	ifPath := filepath.Join(scanUSBPath, fmt.Sprintf("%s:1.%d", filepath.Base(scanUSBPath), foundIfaceIndex))
	md.DriverName = determineDriver(ifPath)

	// 确定控制路径 (cdc-wdm)
	// device.c: detect_path_cdc_wdm_or_qcqmi
	md.ControlPath = findCDCWDM(ifPath)
	if md.ControlPath == "" {
		// 回退到更广泛的搜索
		md.ControlPath = findCDCWDMInUSB(scanUSBPath)
	}
	// device.c 针对 ECM/RNDIS/NCM 的逻辑 (但也适用于 QMI 的 AT 命令)
	atIntf := -1
	if vid == 0x2c7c {
		switch pid {
		case 0x0901, 0x0902, 0x8101: // EC200U, EC200D, RG801H
			atIntf = 2
		case 0x0900: // RG500U
			atIntf = 4
		case 0x6026, 0x6005, 0x6002, 0x6001: // EC200T, EC200A, EC200S, EC100Y
			atIntf = 3
		case 0x6007: // EG915Q/EG800Q
			// if RDNIS_MODEL == 1 { atIntf = 5 } else { atIntf = 3 }
			atIntf = 3 // 暂时假设默认值
		default:
			// 对于 EC20 (pid 0x0125) 和其他型号，典型默认值为 2
			atIntf = 2
		}
	} else if vid == 0x05c6 {
		// 高通默认值
		atIntf = 2
	}

	// 收集该 USB 设备下的全部 ttyUSB 候选口；实际可用性由上层自行探测。
	md.ATPorts = findATPorts(scanUSBPath)

	staticPrimary := ""
	if atIntf != -1 {
		atIfPath := filepath.Join(scanUSBPath, fmt.Sprintf("%s:1.%d", filepath.Base(scanUSBPath), atIntf))
		primary, err := findTTYInInterface(atIfPath)
		if err == nil && primary != "" {
			staticPrimary = primary
		}
	}
	md.ATPort, md.ATPortBackup = chooseStaticATPorts(md.ATPorts, staticPrimary)

	if md.ControlPath == "" {
		// 如果未找到 QMI cdc-wdm，可能是在 ECM 模式下，此时 AT 端口也是控制通道？
		// 但我们的项目是 qmi-cm，所以我们主要严格要求 QMI (cdc-wdm)。
		// 但是，返回我们发现的内容更好。
		// 警告: 如果没有控制路径，功能将受到限制。
	}

	// 查找同一 USB composite device 下的 ALSA 声卡
	md.AudioDevice, md.AudioCardNum = findAudioDevice(scanUSBPath)

	return md, nil
}

// probeIMEIViaQMI 通过 QMI DMS 协议从控制设备读取 IMEI。
// 适用于 ControlPath 非空（即 cdc-wdm 可访问）的 QMI 模式设备。
// 若硬件忙碌或不响应，5 秒超时后静默返回空字符串，不影响发现流程。
func probeIMEIViaQMI(controlPath string) (string, error) {
	controlPath = strings.TrimSpace(controlPath)
	if controlPath == "" {
		return "", fmt.Errorf("control path is empty")
	}

	openCtx, openCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer openCancel()

	client, err := qmi.NewClientWithOptions(openCtx, controlPath, qmi.ClientOptions{})
	if err != nil {
		return "", fmt.Errorf("打开 QMI 设备失败: %w", err)
	}
	defer client.Close()

	dms, err := qmi.NewDMSService(client)
	if err != nil {
		return "", fmt.Errorf("初始化 DMS 服务失败: %w", err)
	}
	defer dms.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	info, err := dms.GetDeviceSerialNumbers(ctx)
	if err != nil {
		return "", fmt.Errorf("QMI DMS 查询 IMEI 失败: %w", err)
	}

	imei := strings.TrimSpace(info.IMEI)
	if imei == "" {
		return "", fmt.Errorf("QMI DMS 返回 IMEI 为空")
	}
	return imei, nil
}

func findCDCWDM(devicePath string) string {
	// 查找 usbmisc 或 usb 子目录
	for _, subDir := range []string{"usbmisc", "usb"} {
		miscPath := filepath.Join(devicePath, subDir)
		entries, err := os.ReadDir(miscPath)
		if err == nil {
			for _, e := range entries {
				if strings.HasPrefix(e.Name(), "cdc-wdm") {
					return filepath.Join("/dev", e.Name())
				}
			}
		}
	}
	return ""
}

func findCDCWDMInUSB(usbPath string) string {
	var result string

	filepath.Walk(resolveUSBPath(usbPath), func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			return nil
		}
		if info.Name() == "usbmisc" || info.Name() == "usb" {
			entries, err := os.ReadDir(path)
			if err == nil {
				for _, e := range entries {
					if strings.HasPrefix(e.Name(), "cdc-wdm") {
						result = filepath.Join("/dev", e.Name())
						return filepath.SkipAll
					}
				}
			}
		}
		return nil
	})

	return result
}

func resolveUSBPath(usbPath string) string {
	p := strings.TrimSpace(usbPath)
	if p == "" {
		return p
	}
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil || strings.TrimSpace(resolved) == "" {
		return p
	}
	return resolved
}

// findATPorts 查找所有与该 USB 设备关联的 ttyUSB 端口
func findATPorts(usbPath string) []string {
	var ports []string

	// usbPath 类似于 /sys/devices/.../usb1/1-1/1-1.2
	// 我们想查找 /sys/devices/.../usb1/1-1/1-1.2/1-1.2:1.*/ttyUSB*
	// 以及某些内核拓扑下的 /tty/ttyUSB* 形态。

	patterns := []string{
		filepath.Join(usbPath, "*", "ttyUSB*"),
		filepath.Join(usbPath, "*", "tty", "ttyUSB*"),
	}

	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		for _, match := range matches {
			ttyName := filepath.Base(match)
			ports = append(ports, filepath.Join("/dev", ttyName))
		}
	}

	return normalizeATPorts(ports)
}

func normalizeATPorts(ports []string) []string {
	if len(ports) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(ports))
	out := make([]string, 0, len(ports))
	for _, port := range ports {
		p := strings.TrimSpace(port)
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func chooseStaticATPorts(atPorts []string, hintedPrimary string) (primary, backup string) {
	ports := normalizeATPorts(atPorts)
	if len(ports) == 0 {
		return "", ""
	}

	ordered := ports
	hint := strings.TrimSpace(hintedPrimary)
	if hint != "" {
		for i, port := range ports {
			if port != hint {
				continue
			}
			ordered = make([]string, 0, len(ports))
			ordered = append(ordered, hint)
			ordered = append(ordered, ports[:i]...)
			ordered = append(ordered, ports[i+1:]...)
			break
		}
	}

	primary = ordered[0]
	if len(ordered) > 1 {
		backup = ordered[1]
	}
	return primary, backup
}

func determineDriver(devicePath string) string {
	driverLink := filepath.Join(devicePath, "driver")
	target, err := os.Readlink(driverLink)
	if err != nil {
		return ""
	}
	return filepath.Base(target)
}

func readHexFile(path string) uint16 {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	val, err := strconv.ParseUint(strings.TrimSpace(string(data)), 16, 16)
	if err != nil {
		return 0
	}
	return uint16(val)
}

func readIntFile(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	val, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0
	}
	return val
}

// findTTYInInterface 在接口路径中查找 tty 目录
func findTTYInInterface(ifPath string) (string, error) {
	// device.c: dir_get_child(pl->path, devname, sizeof(devname), "tty");
	// path/tty/ttyUSBx

	ttyDir := filepath.Join(ifPath, "tty")
	entries, err := os.ReadDir(ttyDir)
	if err == nil {
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), "tty") {
				return filepath.Join("/dev", e.Name()), nil
			}
		}
	}

	// 如果直接读取失败，尝试标准的 Glob
	matches, _ := filepath.Glob(filepath.Join(ifPath, "ttyUSB*"))
	if len(matches) > 0 {
		return filepath.Join("/dev", filepath.Base(matches[0])), nil
	}

	matches2, _ := filepath.Glob(filepath.Join(ifPath, "tty", "ttyUSB*"))
	if len(matches2) > 0 {
		return filepath.Join("/dev", filepath.Base(matches2[0])), nil
	}

	return "", fmt.Errorf("未找到 tty")
}

// findAudioDevice 在 USB composite device 下查找 ALSA 声卡
// 通过遍历 usbPath 下所有接口子目录的 sound/card* 来发现
// 原理：EC20 的 AT 串口和 USB Audio 属于同一 USB composite device，共享相同的 sysfs 父路径
func findAudioDevice(usbPath string) (string, int) {
	usbName := filepath.Base(usbPath)

	// 遍历所有 USB 接口 (如 1-4:1.0, 1-4:1.1, ... 1-4:1.6)
	pattern := filepath.Join(usbPath, usbName+":1.*", "sound", "card*")
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		return "", -1
	}

	// 取第一个匹配的声卡
	cardDir := filepath.Base(matches[0])
	// 从 "cardN" 中解析出 N
	if !strings.HasPrefix(cardDir, "card") {
		return "", -1
	}
	cardNumStr := strings.TrimPrefix(cardDir, "card")
	cardNum, err := strconv.Atoi(cardNumStr)
	if err != nil {
		return "", -1
	}

	alsaDev := fmt.Sprintf("hw:%d,0", cardNum)
	return alsaDev, cardNum
}

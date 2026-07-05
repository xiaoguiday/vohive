//go:build linux
// +build linux

package netcfg

import (
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/iniwex5/netlink"
)

// LinuxConfigurator implements NetworkConfigurator for Linux using netlink
// LinuxConfigurator 使用 netlink 实现 Linux 的 NetworkConfigurator
type LinuxConfigurator struct{}

func NewLinuxConfigurator() *LinuxConfigurator {
	return &LinuxConfigurator{}
}

func (l *LinuxConfigurator) SetIPAddress(ifname string, ip net.IP, prefixLen int) error {
	link, err := netlink.LinkByName(ifname)
	if err != nil {
		return fmt.Errorf("interface %s not found: %w", ifname, err)
	}

	ipNet := &net.IPNet{
		IP:   ip,
		Mask: net.CIDRMask(prefixLen, 32),
	}

	addr := &netlink.Addr{IPNet: ipNet}

	if err := netlink.AddrAdd(link, addr); err != nil {
		if !strings.Contains(err.Error(), "exists") {
			return fmt.Errorf("failed to add address: %w", err)
		}
	}
	return nil
}

func (l *LinuxConfigurator) SetIPv6Address(ifname string, ip net.IP, prefixLen int) error {
	link, err := netlink.LinkByName(ifname)
	if err != nil {
		return fmt.Errorf("interface %s not found: %w", ifname, err)
	}

	ipNet := &net.IPNet{
		IP:   ip,
		Mask: net.CIDRMask(prefixLen, 128),
	}

	addr := &netlink.Addr{IPNet: ipNet}

	if err := netlink.AddrAdd(link, addr); err != nil {
		if !strings.Contains(err.Error(), "exists") {
			return fmt.Errorf("failed to add IPv6 address: %w", err)
		}
	}
	return nil
}

func (l *LinuxConfigurator) FlushAddresses(ifname string) error {
	link, err := netlink.LinkByName(ifname)
	if err != nil {
		return nil // Interface gone
	}

	addrs, err := netlink.AddrList(link, netlink.FAMILY_ALL)
	if err != nil {
		return nil
	}

	for _, addr := range addrs {
		// Ignore errors during cleanup
		_ = netlink.AddrDel(link, &addr)
	}
	return nil
}

func (l *LinuxConfigurator) AddDefaultRoute(ifname string, gateway net.IP) error {
	link, err := netlink.LinkByName(ifname)
	if err != nil {
		return fmt.Errorf("interface %s not found: %w", ifname, err)
	}

	var dst *net.IPNet
	if gateway.To4() != nil {
		dst = &net.IPNet{IP: net.IPv4zero, Mask: net.CIDRMask(0, 32)}
	} else {
		dst = &net.IPNet{IP: net.IPv6zero, Mask: net.CIDRMask(0, 128)}
	}

	route := &netlink.Route{
		LinkIndex: link.Attrs().Index,
		Dst:       dst,
		Gw:        gateway,
		Priority:  5000, // High metric to avoid overriding system default route / 高跃点数避免覆盖系统默认路由
	}

	if err := netlink.RouteAdd(route); err != nil {
		if !strings.Contains(err.Error(), "exists") {
			return fmt.Errorf("failed to add default route: %w", err)
		}
	}
	return nil
}

func (l *LinuxConfigurator) AddDefaultRouteDirect(ifname string, ipv6 bool) error {
	link, err := netlink.LinkByName(ifname)
	if err != nil {
		return fmt.Errorf("interface %s not found: %w", ifname, err)
	}

	var dst *net.IPNet
	if ipv6 {
		dst = &net.IPNet{IP: net.IPv6zero, Mask: net.CIDRMask(0, 128)}
	} else {
		dst = &net.IPNet{IP: net.IPv4zero, Mask: net.CIDRMask(0, 32)}
	}

	route := &netlink.Route{
		LinkIndex: link.Attrs().Index,
		Dst:       dst,
		Priority:  512, // High metric / 高跃点数
	}

	if err := netlink.RouteAdd(route); err != nil {
		if !strings.Contains(err.Error(), "exists") {
			return fmt.Errorf("failed to add default route: %w", err)
		}
	}
	return nil
}

func (l *LinuxConfigurator) FlushRoutes(ifname string) error {
	link, err := netlink.LinkByName(ifname)
	if err != nil {
		return fmt.Errorf("interface %s not found: %w", ifname, err)
	}

	routes, err := netlink.RouteList(link, netlink.FAMILY_ALL)
	if err != nil {
		return fmt.Errorf("failed to list routes: %w", err)
	}

	for _, route := range routes {
		_ = netlink.RouteDel(&route)
	}
	return nil
}

func (l *LinuxConfigurator) BringUp(ifname string) error {
	link, err := netlink.LinkByName(ifname)
	if err != nil {
		return fmt.Errorf("interface %s not found: %w", ifname, err)
	}
	return netlink.LinkSetUp(link)
}

func (l *LinuxConfigurator) BringDown(ifname string) error {
	link, err := netlink.LinkByName(ifname)
	if err != nil {
		return nil // Interface gone
	}
	return netlink.LinkSetDown(link)
}

func (l *LinuxConfigurator) SetMTU(ifname string, mtu int) error {
	link, err := netlink.LinkByName(ifname)
	if err != nil {
		return fmt.Errorf("interface %s not found: %w", ifname, err)
	}
	return netlink.LinkSetMTU(link, mtu)
}

func (l *LinuxConfigurator) GetCurrentIP(ifname string) (net.IP, error) {
	link, err := netlink.LinkByName(ifname)
	if err != nil {
		return nil, fmt.Errorf("interface %s not found: %w", ifname, err)
	}

	addrs, err := netlink.AddrList(link, netlink.FAMILY_V4)
	if err != nil {
		return nil, fmt.Errorf("failed to list addresses: %w", err)
	}

	if len(addrs) == 0 {
		return nil, nil
	}
	return addrs[0].IP, nil
}

func (l *LinuxConfigurator) IsUp(ifname string) (bool, error) {
	link, err := netlink.LinkByName(ifname)
	if err != nil {
		return false, fmt.Errorf("interface %s not found: %w", ifname, err)
	}
	return link.Attrs().Flags&net.FlagUp != 0, nil
}

const resolvConfPath = "/etc/resolv.conf"

func (l *LinuxConfigurator) UpdateDNS(dns1, dns2 string) error {
	var lines []string
	if data, err := os.ReadFile(resolvConfPath); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if !strings.HasPrefix(strings.TrimSpace(line), "nameserver") {
				lines = append(lines, line)
			}
		}
	}

	if dns1 != "" {
		lines = append(lines, "nameserver "+dns1)
	}
	if dns2 != "" && dns2 != dns1 {
		lines = append(lines, "nameserver "+dns2)
	}

	content := strings.Join(lines, "\n")
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return os.WriteFile(resolvConfPath, []byte(content), 0644)
}

func (l *LinuxConfigurator) RestoreDNS() error {
	var lines []string
	if data, err := os.ReadFile(resolvConfPath); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if !strings.HasPrefix(strings.TrimSpace(line), "nameserver") {
				lines = append(lines, line)
			}
		}
	}
	content := strings.Join(lines, "\n")
	return os.WriteFile(resolvConfPath, []byte(content), 0644)
}

// ============================================================================
// QMAP 多路复用 sysfs 操作 (Linux 专属)
// ============================================================================

// AddQMAPMux 向内核 qmi_wwan 驱动申请创建一个 MUX ID 对应的虚拟网卡
// 等同于: echo {muxID} > /sys/class/net/{masterIface}/qmi/add_mux
// 返回值为创建出的虚拟网卡名，例如 "qmimux0"
func (l *LinuxConfigurator) AddQMAPMux(masterIface string, muxID uint8) (string, error) {
	addMuxPath := fmt.Sprintf("/sys/class/net/%s/qmi/add_mux", masterIface)

	// 检查 sysfs 节点是否存在
	if _, err := os.Stat(addMuxPath); os.IsNotExist(err) {
		return "", fmt.Errorf("sysfs 节点 %s 不存在，内核驱动可能不支持 QMAP", addMuxPath)
	}

	// 写入 MuxID 触发内核创建虚拟网卡
	data := fmt.Sprintf("%d\n", muxID)
	if err := os.WriteFile(addMuxPath, []byte(data), 0200); err != nil {
		// 如果已经存在，可能返回 "device or resource busy" 之类的错误
		// 尝试检查对应的虚拟网卡是否已存在
		ifname := l.GetQMAPMuxIface(masterIface, muxID)
		if ifname != "" {
			return ifname, nil // 虚拟网卡已存在，直接使用
		}
		return "", fmt.Errorf("写入 %s 失败: %w", addMuxPath, err)
	}

	// 推导并验证虚拟网卡名
	ifname := l.GetQMAPMuxIface(masterIface, muxID)
	if ifname == "" {
		return "", fmt.Errorf("MuxID %d 的虚拟网卡创建后未找到", muxID)
	}

	return ifname, nil
}

// DelQMAPMux 销毁指定 MuxID 对应的虚拟网卡
// 等同于: echo {muxID} > /sys/class/net/{masterIface}/qmi/del_mux
func (l *LinuxConfigurator) DelQMAPMux(masterIface string, muxID uint8) error {
	delMuxPath := fmt.Sprintf("/sys/class/net/%s/qmi/del_mux", masterIface)

	if _, err := os.Stat(delMuxPath); os.IsNotExist(err) {
		return nil // 节点不存在就认为无需清理
	}

	data := fmt.Sprintf("%d\n", muxID)
	if err := os.WriteFile(delMuxPath, []byte(data), 0200); err != nil {
		return fmt.Errorf("写入 %s 失败: %w", delMuxPath, err)
	}
	return nil
}

// GetQMAPMuxIface 根据 MuxID 推导虚拟网卡接口名
// qmi_wwan 驱动的命名规则: qmimux{muxID - 1}，即 MuxID=1 对应 qmimux0
func (l *LinuxConfigurator) GetQMAPMuxIface(masterIface string, muxID uint8) string {
	// qmi_wwan 驱动创建的虚拟网卡名为 qmimux{N}，N 从 0 开始递增
	// MuxID=1 => qmimux0, MuxID=2 => qmimux1, ...
	// 但实际命名取决于创建顺序而非 MuxID 值
	// 因此我们需要扫描 sysfs 确认

	// 方式1: 扫描 /sys/class/net/ 下所有 qmimux* 开头的接口
	// 并检查其 upper 设备是否是 masterIface
	entries, err := os.ReadDir("/sys/class/net")
	if err != nil {
		return ""
	}

	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "qmimux") {
			continue
		}
		// 检查该虚拟网卡是否属于目标物理网卡
		// 读取 /sys/class/net/qmimuxN/qmap/mux_id
		muxIDPath := fmt.Sprintf("/sys/class/net/%s/qmap/mux_id", name)
		if data, err := os.ReadFile(muxIDPath); err == nil {
			val := strings.TrimSpace(string(data))
			expected := fmt.Sprintf("0x%x", muxID)
			expectedDec := fmt.Sprintf("%d", muxID)
			if val == expected || val == expectedDec {
				return name
			}
		}
	}

	// 方式2: 如果上面没找到，尝试直接用常规命名推导
	// 这是一个备选逻辑: 假设 MuxID=1 => qmimux0
	candidate := fmt.Sprintf("qmimux%d", muxID-1)
	if _, err := os.Stat(fmt.Sprintf("/sys/class/net/%s", candidate)); err == nil {
		return candidate
	}

	return ""
}

// EnableRawIP 在物理网卡上开启 Raw IP 模式（QMAP 前置条件）
// 等同于: echo Y > /sys/class/net/{ifname}/qmi/raw_ip
func (l *LinuxConfigurator) EnableRawIP(ifname string) error {
	rawIPPath := fmt.Sprintf("/sys/class/net/%s/qmi/raw_ip", ifname)

	if _, err := os.Stat(rawIPPath); os.IsNotExist(err) {
		return nil // 内核驱动不支持，跳过
	}

	// 检查是否已经开启
	if content, err := os.ReadFile(rawIPPath); err == nil {
		s := strings.TrimSpace(string(content))
		if s == "Y" || s == "y" || s == "1" {
			return nil // 已开启
		}
	}

	// 开启前需要先关闭网卡
	_ = l.BringDown(ifname)

	if err := os.WriteFile(rawIPPath, []byte("Y\n"), 0644); err != nil {
		return fmt.Errorf("开启 raw_ip 失败: %w", err)
	}

	return nil
}

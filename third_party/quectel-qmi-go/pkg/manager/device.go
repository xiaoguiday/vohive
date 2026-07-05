package manager

import "fmt"

// ModemDevice 表示由发现流程或调用方注入的 modem 设备信息。
// 该结构是库内唯一的设备描述类型，pkg/device 只负责返回它，不再重复定义。
type ModemDevice struct {
	ControlPath  string
	NetInterface string

	USBPath   string
	VendorID  uint16
	ProductID uint16

	DriverName string

	ATPorts      []string
	ATPort       string
	ATPortBackup string

	AudioDevice  string
	AudioCardNum int
}

func (m ModemDevice) String() string {
	s := fmt.Sprintf("%s (%s) [%04x:%04x] driver=%s AT=%s Backup=%s",
		m.ControlPath, m.NetInterface, m.VendorID, m.ProductID, m.DriverName, m.ATPort, m.ATPortBackup)
	if m.AudioDevice != "" {
		s += fmt.Sprintf(" Audio=%s", m.AudioDevice)
	}
	return s
}

var discoverModemsFn func() ([]ModemDevice, error)

// SetDeviceDiscoverer 注册库内可复用的设备发现实现。
// 由 pkg/device 在 init 阶段注入，避免 manager 与 device 包之间形成循环依赖。
func SetDeviceDiscoverer(fn func() ([]ModemDevice, error)) {
	discoverModemsFn = fn
}

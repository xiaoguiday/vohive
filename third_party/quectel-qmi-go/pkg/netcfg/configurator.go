package netcfg

import "net"

// NetworkConfigurator defines the interface for OS-specific network operations
// NetworkConfigurator 定义了特定于操作系统的网络操作接口
type NetworkConfigurator interface {
	// SetIPAddress configures an IPv4 address on the interface
	// SetIPAddress 在接口上配置 IPv4 地址
	SetIPAddress(ifname string, ip net.IP, prefixLen int) error

	// SetIPv6Address configures an IPv6 address on the interface
	// SetIPv6Address 在接口上配置 IPv6 地址
	SetIPv6Address(ifname string, ip net.IP, prefixLen int) error

	// FlushAddresses removes all IP addresses from the interface
	// FlushAddresses 移除接口上的所有 IP 地址
	FlushAddresses(ifname string) error

	// AddDefaultRoute adds a default route via the given gateway
	// AddDefaultRoute 添加通过给定网关的默认路由
	AddDefaultRoute(ifname string, gateway net.IP) error

	// AddDefaultRouteDirect adds a default route directly to the interface
	// AddDefaultRouteDirect 直接向接口添加默认路由
	AddDefaultRouteDirect(ifname string, ipv6 bool) error

	// FlushRoutes removes all routes for the interface
	// FlushRoutes 移除接口的所有路由
	FlushRoutes(ifname string) error

	// BringUp brings the interface up
	// BringUp 启动接口
	BringUp(ifname string) error

	// BringDown brings the interface down
	// BringDown 关闭接口
	BringDown(ifname string) error

	// SetMTU sets the MTU for the interface
	// SetMTU 设置接口的 MTU
	SetMTU(ifname string, mtu int) error

	// GetCurrentIP returns the current IPv4 address of the interface
	// GetCurrentIP 返回接口当前的 IPv4 地址
	GetCurrentIP(ifname string) (net.IP, error)

	// IsUp checks if the interface is up
	// IsUp 检查接口是否已启动
	IsUp(ifname string) (bool, error)

	// UpdateDNS updates the system DNS configuration
	// UpdateDNS 更新系统 DNS 配置
	UpdateDNS(dns1, dns2 string) error

	// RestoreDNS restores the system DNS configuration
	// RestoreDNS 恢复系统 DNS 配置
	RestoreDNS() error

	// AddQMAPMux 创建 QMAP 多路复用虚拟网卡，返回虚拟网卡名
	AddQMAPMux(masterIface string, muxID uint8) (string, error)

	// DelQMAPMux 销毁 QMAP 虚拟网卡
	DelQMAPMux(masterIface string, muxID uint8) error

	// GetQMAPMuxIface 根据 MuxID 查询虚拟网卡名
	GetQMAPMuxIface(masterIface string, muxID uint8) string

	// EnableRawIP 在网卡上开启 Raw IP 模式
	EnableRawIP(ifname string) error
}

var currentConfigurator NetworkConfigurator

// SetConfigurator sets the active network configurator
// SetConfigurator 设置活动的网络配置器
func SetConfigurator(c NetworkConfigurator) {
	currentConfigurator = c
}

// GetConfigurator returns the active network configurator
// GetConfigurator 返回活动的网络配置器
func GetConfigurator() NetworkConfigurator {
	if currentConfigurator == nil {
		// Auto-detect platform implementation / 自动检测平台实现
		currentConfigurator = GetPlatformConfigurator()
	}
	return currentConfigurator
}

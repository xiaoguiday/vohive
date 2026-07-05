package netcfg

import (
	"fmt"
	"net"
	"os/exec"
	"strconv"
)

// DarwinConfigurator implements NetworkConfigurator for macOS
// DarwinConfigurator 为 macOS 实现 NetworkConfigurator
type DarwinConfigurator struct{}

func NewDarwinConfigurator() *DarwinConfigurator {
	return &DarwinConfigurator{}
}

func (d *DarwinConfigurator) run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("command %s failed: %s, output: %s", name, err, string(out))
	}
	return nil
}

func (d *DarwinConfigurator) SetIPAddress(ifname string, ip net.IP, prefixLen int) error {
	// ifconfig ifname inet IP/Prefix alias
	return d.run("ifconfig", ifname, "inet", fmt.Sprintf("%s/%d", ip.String(), prefixLen), "alias")
}

func (d *DarwinConfigurator) SetIPv6Address(ifname string, ip net.IP, prefixLen int) error {
	// ifconfig ifname inet6 IP/Prefix alias
	return d.run("ifconfig", ifname, "inet6", fmt.Sprintf("%s/%d", ip.String(), prefixLen), "alias")
}

func (d *DarwinConfigurator) FlushAddresses(ifname string) error {
	// Can't easily flush all, so we might skip or rely on down/up
	// 无法轻易清除所有，所以我们可能跳过或依赖 down/up
	return nil
}

func (d *DarwinConfigurator) AddDefaultRoute(ifname string, gateway net.IP) error {
	// route add default gateway -interface ifname
	if gateway.To4() != nil {
		return d.run("route", "add", "default", gateway.String(), "-interface", ifname)
	}
	return d.run("route", "add", "-inet6", "default", gateway.String(), "-interface", ifname)
}

func (d *DarwinConfigurator) AddDefaultRouteDirect(ifname string, ipv6 bool) error {
	if ipv6 {
		return d.run("route", "add", "-inet6", "default", "-interface", ifname)
	}
	return d.run("route", "add", "default", "-interface", ifname)
}

func (d *DarwinConfigurator) FlushRoutes(ifname string) error {
	// macOS doesn't have a simple flush per interface
	return nil
}

func (d *DarwinConfigurator) BringUp(ifname string) error {
	return d.run("ifconfig", ifname, "up")
}

func (d *DarwinConfigurator) BringDown(ifname string) error {
	return d.run("ifconfig", ifname, "down")
}

func (d *DarwinConfigurator) SetMTU(ifname string, mtu int) error {
	return d.run("ifconfig", ifname, "mtu", strconv.Itoa(mtu))
}

func (d *DarwinConfigurator) GetCurrentIP(ifname string) (net.IP, error) {
	iface, err := net.InterfaceByName(ifname)
	if err != nil {
		return nil, err
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return nil, err
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP, nil
			}
		}
	}
	return nil, nil
}

func (d *DarwinConfigurator) IsUp(ifname string) (bool, error) {
	iface, err := net.InterfaceByName(ifname)
	if err != nil {
		return false, err
	}
	return iface.Flags&net.FlagUp != 0, nil
}

func (d *DarwinConfigurator) UpdateDNS(dns1, dns2 string) error {
	// macOS uses networksetup or scutil, modifying /etc/resolv.conf is not recommended but might work temporarily
	// macOS 使用 networksetup 或 scutil，修改 /etc/resolv.conf 不推荐但可能暂时有效
	// For simplicity in this CLI tool, we skip system-wide DNS modification on macOS
	return nil
}

func (d *DarwinConfigurator) RestoreDNS() error {
	return nil
}

// QMAP 多路复用在 macOS 上不支持
func (d *DarwinConfigurator) AddQMAPMux(masterIface string, muxID uint8) (string, error) {
	return "", fmt.Errorf("QMAP 多路复用在 macOS 上不可用")
}
func (d *DarwinConfigurator) DelQMAPMux(masterIface string, muxID uint8) error       { return nil }
func (d *DarwinConfigurator) GetQMAPMuxIface(masterIface string, muxID uint8) string { return "" }
func (d *DarwinConfigurator) EnableRawIP(ifname string) error                        { return nil }

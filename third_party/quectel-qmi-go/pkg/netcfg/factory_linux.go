//go:build linux
// +build linux

package netcfg

// GetPlatformConfigurator returns the Linux configurator
// GetPlatformConfigurator 返回 Linux 配置器
func GetPlatformConfigurator() NetworkConfigurator {
	return NewLinuxConfigurator()
}

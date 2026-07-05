//go:build windows
// +build windows

package netcfg

// GetPlatformConfigurator returns the Windows configurator
// GetPlatformConfigurator 返回 Windows 配置器
func GetPlatformConfigurator() NetworkConfigurator {
	return NewWindowsConfigurator()
}

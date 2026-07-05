//go:build darwin
// +build darwin

package netcfg

// GetPlatformConfigurator returns the macOS configurator
// GetPlatformConfigurator 返回 macOS 配置器
func GetPlatformConfigurator() NetworkConfigurator {
	return NewDarwinConfigurator()
}

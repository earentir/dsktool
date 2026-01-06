package main

// getBlockDeviceSizePlatform gets disk size, platform-specific
func getBlockDeviceSizePlatform(devPath string) (int64, error) {
	return getBlockDeviceSizePlatformImpl(devPath)
}

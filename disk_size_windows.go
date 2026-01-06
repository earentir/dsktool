//go:build windows

package main

import "fmt"

// getBlockDeviceSizePlatformImpl is the Windows implementation
func getBlockDeviceSizePlatformImpl(devPath string) (int64, error) {
	return 0, fmt.Errorf("disk size detection not implemented on Windows")
}

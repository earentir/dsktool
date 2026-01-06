//go:build linux

package main

import (
	"fmt"
	"os"
	"strings"
)

func getDiskListDataPlatform() []DiskInfo {
	var disks []DiskInfo
	blockDevices, err := os.ReadDir("/sys/class/block")
	if err != nil {
		return disks
	}

	excludePrefixes := []string{"loop", "zram", "ram"}

	for _, bd := range blockDevices {
		devName := bd.Name()

		shouldContinue := false
		for _, prefix := range excludePrefixes {
			if strings.HasPrefix(devName, prefix) {
				shouldContinue = true
				break
			}
		}

		if shouldContinue {
			continue
		}

		devPath := "/dev/" + devName
		totalSize, err := getBlockDeviceSize(devPath)

		var sizeStr string
		var sizeBytes int64
		if err != nil {
			sizeStr = fmt.Sprintf("Error: %v", err)
			sizeBytes = 0
		} else {
			sizeBytes = totalSize
			sizeStr = formatBytes(totalSize)
		}

		// Attempt to find a mount point for this device
		mountPoint, err := findMountPointForDevice(devPath)
		var mountInfo string
		var mounted bool
		if err != nil {
			mountInfo = "(No filesystem mount found)"
			mounted = false
		} else {
			mounted = true
			totalFs, usedFs, freeFs, err := getFsSpace(mountPoint)
			if err != nil {
				mountInfo = fmt.Sprintf("(mounted on %s) - Error reading filesystem", mountPoint)
			} else {
				mountInfo = fmt.Sprintf("(mounted on %s) - Total: %s, Used: %s, Free: %s",
					mountPoint, formatBytes(totalFs), formatBytes(usedFs), formatBytes(freeFs))
			}
		}

		diskType := getDiskType(devPath)

		disks = append(disks, DiskInfo{
			Path:      devPath,
			Size:      sizeBytes,
			SizeStr:   sizeStr,
			MountInfo: mountInfo,
			Mounted:   mounted,
			DiskType:  diskType,
		})
	}

	return disks
}

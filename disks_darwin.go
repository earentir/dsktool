//go:build darwin

package main

import (
	"fmt"
	"os"
	"strings"
)

func getDiskListDataPlatform() []DiskInfo {
	var disks []DiskInfo
	entries, err := os.ReadDir("/dev")
	if err != nil {
		return disks
	}

	diskMap := make(map[string]bool)
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, "disk") && len(name) > 4 {
			if len(name) == 5 {
				if name[4] >= '0' && name[4] <= '9' {
					diskMap[name] = true
				}
			} else if len(name) > 5 && name[4] >= '0' && name[4] <= '9' {
				if idx := strings.Index(name[4:], "s"); idx > 0 {
					baseName := name[:4+idx]
					diskMap[baseName] = true
				} else {
					allDigits := true
					for i := 4; i < len(name); i++ {
						if name[i] < '0' || name[i] > '9' {
							allDigits = false
							break
						}
					}
					if allDigits {
						diskMap[name] = true
					}
				}
			}
		}
	}

	for diskName := range diskMap {
		devPath := "/dev/" + diskName
		totalSize, err := getBlockDeviceSize(devPath)

		var sizeStr string
		var sizeBytes int64
		if err != nil {
			errStr := err.Error()
			if strings.Contains(errStr, "permission") || strings.Contains(errStr, "not permitted") {
				sizeStr = "(Size unavailable: requires root access)"
				sizeBytes = 0
			} else if strings.Contains(errStr, "resource busy") || strings.Contains(errStr, "device busy") {
				rawPath := strings.Replace(devPath, "/dev/disk", "/dev/rdisk", 1)
				totalSize, err = getBlockDeviceSizeFromPath(rawPath)
				if err != nil {
					sizeStr = "(Device busy, could not read size)"
					sizeBytes = 0
				} else {
					sizeBytes = totalSize
					sizeStr = formatBytes(totalSize)
				}
			} else {
				sizeStr = fmt.Sprintf("Error: %v", err)
				sizeBytes = 0
			}
		} else {
			sizeBytes = totalSize
			sizeStr = formatBytes(totalSize)
		}

		// Try to find mount point
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

//go:build darwin

package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func getDiskListDataPlatform() []DiskInfo {
	var disks []DiskInfo

	// Start with diskutil to get disks it knows about
	diskMap := getDiskListFromDiskutil()

	// Also scan /dev for any disks diskutil might miss (especially unmounted USB disks)
	// This is important because diskutil may not show unmounted/ejected disks
	entries, err := os.ReadDir("/dev")
	if err == nil {
		for _, entry := range entries {
			name := entry.Name()
			var baseName string

			// Check for "disk" entries (e.g., disk0, disk1, disk2s1, disk10, disk10s1)
			if strings.HasPrefix(name, "disk") && len(name) > 4 {
				// Extract base disk name (e.g., "disk2" from "disk2s1", "disk10" from "disk10s1")
				baseName = name
				if idx := strings.Index(name[4:], "s"); idx > 0 {
					// Has partition suffix, extract base
					baseName = name[:4+idx]
				}
			} else if strings.HasPrefix(name, "rdisk") && len(name) > 5 {
				// Also check for "rdisk" entries (raw devices, especially for unmounted disks)
				// Extract base disk name (e.g., "disk2" from "rdisk2s1")
				baseName = "disk" + name[5:]
				if idx := strings.Index(name[5:], "s"); idx > 0 {
					// Has partition suffix, extract base
					baseName = "disk" + name[5:5+idx]
				}
			} else {
				continue
			}

			// Validate it's a valid disk name (starts with digit after "disk")
			if len(baseName) >= 5 && baseName[4] >= '0' && baseName[4] <= '9' {
				// Check if rest are digits (for multi-digit disk numbers like disk10, disk100)
				allDigits := true
				for i := 4; i < len(baseName); i++ {
					if baseName[i] < '0' || baseName[i] > '9' {
						allDigits = false
						break
					}
				}
				if allDigits {
					diskMap[baseName] = true
				}
			}
		}
	}

	for diskName := range diskMap {
		devPath := "/dev/" + diskName
		rawPath := "/dev/r" + diskName // rdisk version

		// Try to get size - prefer rdisk for unmounted disks
		var totalSize int64
		var sizeStr string
		var sizeBytes int64

		// First try rdisk (works better for unmounted disks)
		totalSize, err = getBlockDeviceSizeFromPath(rawPath)
		if err != nil {
			// If rdisk fails, try regular disk
			totalSize, err = getBlockDeviceSize(devPath)
			if err != nil {
				// Both failed - but include the disk anyway
				errStr := err.Error()
				if strings.Contains(errStr, "permission") || strings.Contains(errStr, "not permitted") {
					sizeStr = "(Size unavailable: requires root access)"
					sizeBytes = 0
				} else if strings.Contains(errStr, "resource busy") || strings.Contains(errStr, "device busy") {
					sizeStr = "(Device busy/mounted)"
					sizeBytes = 0
				} else {
					// Include disk even if we can't get size (might be unmounted or inaccessible)
					sizeStr = "(Size unavailable)"
					sizeBytes = 0
				}
			} else {
				sizeBytes = totalSize
				sizeStr = formatBytes(totalSize)
			}
		} else {
			sizeBytes = totalSize
			sizeStr = formatBytes(totalSize)
		}

		// Try to find mount point (check both disk and rdisk paths)
		mountPoint, err := findMountPointForDevice(devPath)
		if err != nil {
			// Also try rdisk path for mount detection
			mountPoint, err = findMountPointForDevice(rawPath)
		}
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

		// Use rdisk path for display (required for write operations on macOS)
		displayPath := rawPath

		disks = append(disks, DiskInfo{
			Path:      displayPath,
			Size:      sizeBytes,
			SizeStr:   sizeStr,
			MountInfo: mountInfo,
			Mounted:   mounted,
			DiskType:  diskType,
		})
	}

	return disks
}

// getDiskListFromDiskutil uses diskutil list to get all disks
func getDiskListFromDiskutil() map[string]bool {
	diskMap := make(map[string]bool)

	// Run diskutil list to get all disks
	cmd := exec.Command("diskutil", "list")
	output, err := cmd.Output()
	if err != nil {
		// If diskutil fails, return empty map (will fall back to /dev scanning)
		return diskMap
	}

	// Parse diskutil output
	// Format: /dev/disk0 (internal, physical): ...
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := scanner.Text()
		// Look for lines that start with /dev/disk
		if strings.HasPrefix(line, "/dev/disk") {
			// Extract disk name (e.g., "/dev/disk0" -> "disk0")
			parts := strings.Fields(line)
			if len(parts) > 0 {
				devPath := parts[0]
				if strings.HasPrefix(devPath, "/dev/disk") {
					diskName := strings.TrimPrefix(devPath, "/dev/")
					// Extract base name (without partition suffix)
					if idx := strings.Index(diskName[4:], "s"); idx > 0 {
						diskName = diskName[:4+idx]
					}
					// Validate it's a valid disk name
					if len(diskName) >= 5 && diskName[4] >= '0' && diskName[4] <= '9' {
						allDigits := true
						for i := 4; i < len(diskName); i++ {
							if diskName[i] < '0' || diskName[i] > '9' {
								allDigits = false
								break
							}
						}
						if allDigits {
							diskMap[diskName] = true
						}
					}
				}
			}
		}
	}

	return diskMap
}

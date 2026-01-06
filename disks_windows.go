//go:build windows

package main

import (
	"fmt"
	"golang.org/x/sys/windows"
)

func getDiskListDataPlatform() []DiskInfo {
	var disks []DiskInfo
	driveBits, err := windows.GetLogicalDrives()
	if err != nil {
		return disks
	}

	for i := 0; i < 26; i++ {
		if driveBits&(1<<uint(i)) != 0 {
			driveLetter := string(rune('A' + i))
			diskPath := fmt.Sprintf("%s:\\", driveLetter)
			diskType := getDiskType(diskPath)

			disks = append(disks, DiskInfo{
				Path:      diskPath,
				Size:      0,
				SizeStr:   "Unknown",
				MountInfo: "",
				Mounted:   false,
				DiskType:  diskType,
			})
		}
	}

	return disks
}

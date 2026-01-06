//go:build darwin || linux

package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"strings"
)

// getPartitionsDataDirectPlatform gets partition data directly from disk
func getPartitionsDataDirectPlatform(diskPath string) ([]PartitionInfo, error) {
	// On macOS, try rdisk first, fall back to disk if needed
	var file *os.File
	var err error
	readPath := diskPath

	// Try opening with the provided path first
	file, err = os.Open(readPath)
	if err != nil {
		// If rdisk fails and we're on macOS, try disk
		if strings.HasPrefix(diskPath, "/dev/rdisk") {
			readPath = strings.Replace(diskPath, "/dev/rdisk", "/dev/disk", 1)
			file, err = os.Open(readPath)
		}
		if err != nil {
			return nil, fmt.Errorf("error opening disk (tried %s): %w", readPath, err)
		}
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("error stating disk: %w", err)
	}

	mode := info.Mode()
	if (mode & os.ModeDevice) == 0 {
		return nil, fmt.Errorf("%s is not a device file", diskPath)
	}

	sectorSize := uint64(getSectorSize(file))

	// Check if GPT or MBR
	isGPT := isGPTDiskSafe(file)
	var partitions []PartitionInfo
	var partErr error
	if isGPT {
		partitions, partErr = getGPTPartitionsData(file, diskPath, sectorSize)
	} else {
		partitions, partErr = getMBRPartitionsData(file, diskPath, sectorSize)
	}

	// If we got an error reading partitions, try to at least show unused space
	if partErr != nil {
		unused, unusedErr := getUnusedSpaceForEmptyDisk(file, diskPath, sectorSize, isGPT)
		if unusedErr == nil && len(unused) > 0 {
			return unused, nil
		}
		return nil, partErr
	}

	// If no partitions found, show entire disk as unused
	if len(partitions) == 0 {
		unused, unusedErr := getUnusedSpaceForEmptyDisk(file, diskPath, sectorSize, isGPT)
		if unusedErr == nil && len(unused) > 0 {
			return unused, nil
		}
		// If we can't get unused space, still return empty list (not an error)
		// This allows the caller to handle it
		return []PartitionInfo{}, nil
	}

	return partitions, nil
}

// getUnusedSpaceForEmptyDisk returns unused space for a disk with no partitions
func getUnusedSpaceForEmptyDisk(file *os.File, diskPath string, sectorSize uint64, isGPT bool) ([]PartitionInfo, error) {
	// Get total disk size
	var totalDiskSectors uint64
	if stat, err := file.Stat(); err == nil {
		totalDiskSectors = uint64(stat.Size()) / sectorSize
	} else {
		if size, err := getBlockDeviceSize(diskPath); err == nil {
			totalDiskSectors = uint64(size) / sectorSize
		} else {
			return nil, fmt.Errorf("cannot determine disk size: %w", err)
		}
	}

	if totalDiskSectors == 0 {
		return nil, fmt.Errorf("disk size is zero")
	}

	// Determine start LBA based on partition table type
	// GPT: partitions start at LBA 34 (after protective MBR + GPT header + partition table)
	// MBR: partitions start at LBA 1 (after MBR at LBA 0)
	var unusedStart uint64
	if isGPT {
		unusedStart = 34 // GPT partition table typically ends around LBA 33
	} else {
		unusedStart = 1 // MBR partitions start at LBA 1
	}

	if unusedStart >= totalDiskSectors {
		return nil, fmt.Errorf("disk too small")
	}

	unusedSectors := totalDiskSectors - unusedStart

	return []PartitionInfo{
		{
			Number:       1,
			Name:         "Unused",
			Type:         "  ",
			FileSystem:   "",
			Size:         formatBytes(unusedSectors * sectorSize),
			FirstLBA:     unusedStart,
			LastLBA:      totalDiskSectors - 1,
			TotalSectors: unusedSectors,
			SectorSize:   sectorSize,
			Unused:       true,
		},
	}, nil
}

func getGPTPartitionsData(file *os.File, diskDevice string, sectorSize uint64) ([]PartitionInfo, error) {
	var partitions []PartitionInfo

	headerBytes := make([]byte, 512)
	_, err := file.ReadAt(headerBytes, 512)
	if err != nil {
		return nil, fmt.Errorf("error reading GPT header: %w", err)
	}

	header := gptHeader{}
	err = binary.Read(bytes.NewReader(headerBytes), binary.LittleEndian, &header)
	if err != nil {
		return nil, fmt.Errorf("error parsing GPT header: %w", err)
	}

	if header.HeaderSize < 92 || int(header.HeaderSize) > len(headerBytes) {
		return nil, fmt.Errorf("invalid GPT header size: %d", header.HeaderSize)
	}

	tableBytes := uint64(header.NumPartEntries) * uint64(header.PartEntrySize)
	table := make([]byte, tableBytes)
	_, err = file.ReadAt(table, int64(header.PartitionEntryLBA*512))
	if err != nil {
		return nil, fmt.Errorf("error reading GPT entries: %w", err)
	}

	partID := 0
	for i := uint32(0); i < header.NumPartEntries; i++ {
		off := uint64(i) * uint64(header.PartEntrySize)
		partition := gptPartition{}
		err := binary.Read(bytes.NewReader(table[off:off+uint64(header.PartEntrySize)]), binary.LittleEndian, &partition)
		if err != nil {
			continue
		}

		// Skip empty entries
		if isAllZero(partition.TypeGUID[:]) || partition.FirstLBA == 0 {
			continue
		}

		partID++
		fsType := detectFileSystem(file, int64(partition.FirstLBA*uint64(sectorSize)))
		totalSectors := partition.LastLBA - partition.FirstLBA + 1

		partName := decodeUTF16LE(partition.PartitionName[:])
		typeGUID := guidToString(partition.TypeGUID[:])
		uniqueGUID := guidToString(partition.UniqueGUID[:])

		// Get mount point for this partition
		// On macOS, partitions are like /dev/rdisk1s1 or /dev/disk1s1, on Linux like /dev/sda1
		var partitionPath string
		// Use the original diskDevice path format (rdisk or disk)
		if strings.Contains(diskDevice, "disk") || strings.Contains(diskDevice, "rdisk") {
			// macOS format: /dev/rdisk1 -> /dev/rdisk1s1 or /dev/disk1 -> /dev/disk1s1
			partitionPath = fmt.Sprintf("%ss%d", diskDevice, partID)
		} else {
			// Linux format: /dev/sda -> /dev/sda1
			partitionPath = fmt.Sprintf("%s%d", diskDevice, partID)
		}
		// Try to find mount point - on macOS, try both disk and rdisk versions
		mountPoint, err := findMountPointForDevice(partitionPath)
		if err != nil && strings.Contains(partitionPath, "rdisk") {
			// If rdisk path fails, try disk path
			diskPath := strings.Replace(partitionPath, "/dev/rdisk", "/dev/disk", 1)
			mountPoint, err = findMountPointForDevice(diskPath)
		} else if err != nil && strings.Contains(partitionPath, "disk") && !strings.Contains(partitionPath, "rdisk") {
			// If disk path fails, try rdisk path
			rdiskPath := strings.Replace(partitionPath, "/dev/disk", "/dev/rdisk", 1)
			mountPoint, err = findMountPointForDevice(rdiskPath)
		}
		var mountInfo string
		var mounted bool
		if err != nil {
			mounted = false
			mountInfo = ""
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

		partitions = append(partitions, PartitionInfo{
			Number:       partID,
			Name:         partName,
			Type:         typeGUID,
			FileSystem:   fsType,
			Size:         formatBytes(totalSectors * sectorSize),
			FirstLBA:     partition.FirstLBA,
			LastLBA:      partition.LastLBA,
			TotalSectors: totalSectors,
			SectorSize:   sectorSize,
			TypeGUID:     typeGUID,
			UniqueGUID:   uniqueGUID,
			MountPoint:   mountPoint,
			MountInfo:    mountInfo,
			Mounted:      mounted,
		})
	}

	// Calculate unused space at the end
	// Get total disk size
	var totalDiskSectors uint64
	if stat, err := file.Stat(); err == nil {
		totalDiskSectors = uint64(stat.Size()) / sectorSize
	} else {
		if size, err := getBlockDeviceSize(diskDevice); err == nil {
			totalDiskSectors = uint64(size) / sectorSize
		}
	}

	// Find the last partition's end
	var lastPartitionEnd uint64
	for _, part := range partitions {
		// Calculate end LBA (LastLBA might be 0, so calculate from FirstLBA + TotalSectors)
		partEnd := part.FirstLBA + part.TotalSectors - 1
		if part.LastLBA > 0 && part.LastLBA > partEnd {
			partEnd = part.LastLBA
		}
		if partEnd > lastPartitionEnd {
			lastPartitionEnd = partEnd
		}
	}

	// Add unused space if there's any
	// Handle case where there are no partitions (lastPartitionEnd = 0)
	// GPT typically starts partitions at LBA 34 (after protective MBR + GPT header + partition table)
	var unusedStart uint64
	if len(partitions) == 0 {
		// No partitions - GPT partitions start at LBA 34
		unusedStart = 34
	} else {
		unusedStart = lastPartitionEnd + 1
	}

	// Always try to add unused space if we have disk size info
	// If no partitions and we can't get size, try harder to get it
	if len(partitions) == 0 {
		if totalDiskSectors == 0 {
			// Try alternative method to get disk size
			if size, err := getBlockDeviceSize(diskDevice); err == nil {
				totalDiskSectors = uint64(size) / sectorSize
			}
		}

		if totalDiskSectors > 0 && unusedStart < totalDiskSectors {
			unusedSectors := totalDiskSectors - unusedStart
			partitions = append(partitions, PartitionInfo{
				Number:       1,
				Name:         "Unused",
				Type:         "  ",
				FileSystem:   "",
				Size:         formatBytes(unusedSectors * sectorSize),
				FirstLBA:     unusedStart,
				LastLBA:      totalDiskSectors - 1,
				TotalSectors: unusedSectors,
				SectorSize:   sectorSize,
				Unused:       true,
			})
		} else {
			// Still show unused space even if we can't get exact size
			// Use reasonable placeholder values that won't cause CHS calculation issues
			// Assume a minimum 1GB disk for display purposes
			placeholderSectors := uint64(1024 * 1024 * 1024 / 512) // 1GB in sectors
			partitions = append(partitions, PartitionInfo{
				Number:       1,
				Name:         "Unused",
				Type:         "  ",
				FileSystem:   "",
				Size:         "Unknown",
				FirstLBA:     unusedStart,
				LastLBA:      unusedStart + placeholderSectors - 1,
				TotalSectors: placeholderSectors,
				SectorSize:   sectorSize,
				Unused:       true,
			})
		}
	} else if totalDiskSectors > 0 && unusedStart < totalDiskSectors {
		unusedSectors := totalDiskSectors - unusedStart
		if unusedSectors > 0 {
			partitions = append(partitions, PartitionInfo{
				Number:       len(partitions) + 1,
				Name:         "Unused",
				Type:         "  ",
				FileSystem:   "",
				Size:         formatBytes(unusedSectors * sectorSize),
				FirstLBA:     unusedStart,
				LastLBA:      totalDiskSectors - 1,
				TotalSectors: unusedSectors,
				SectorSize:   sectorSize,
				Unused:       true,
			})
		}
	}

	return partitions, nil
}

func getMBRPartitionsData(file *os.File, diskDevice string, sectorSize uint64) ([]PartitionInfo, error) {
	var partitions []PartitionInfo

	_, err := file.Seek(0, io.SeekStart)
	if err != nil {
		return nil, fmt.Errorf("error seeking to MBR: %w", err)
	}

	mbr := mbrStruct{}
	err = binary.Read(file, binary.LittleEndian, &mbr)
	if err != nil {
		return nil, fmt.Errorf("error reading MBR: %w", err)
	}

	if mbr.Signature != 0xAA55 {
		return nil, fmt.Errorf("invalid MBR signature (0x%04X)", mbr.Signature)
	}

	var sizeBytes int64
	if stat, err := file.Stat(); err == nil {
		sizeBytes = stat.Size()
	}
	if sizeBytes <= 0 {
		if size, err := getBlockDeviceSize(diskDevice); err == nil {
			sizeBytes = size
		}
	}

	partNum := 1
	for _, part := range mbr.Partitions {
		if part.Sectors != 0 {
			if isExtendedType(part.Type) {
				// Extended partition - read logical partitions
				logicalParts, err := readEBRChain(file, sizeBytes, sectorSize, part.FirstSector)
				if err == nil {
					for _, logicalPart := range logicalParts {
						fsType := detectFileSystem(file, int64(logicalPart.FirstSector*uint32(sectorSize)))
				// Get mount point for this partition
				var partitionPath string
				if strings.Contains(diskDevice, "disk") || strings.Contains(diskDevice, "rdisk") {
					partitionPath = fmt.Sprintf("%ss%d", diskDevice, partNum)
				} else {
					partitionPath = fmt.Sprintf("%s%d", diskDevice, partNum)
				}
						// Try to find mount point - on macOS, try both disk and rdisk versions
						mountPoint, mountErr := findMountPointForDevice(partitionPath)
						if mountErr != nil && strings.Contains(partitionPath, "rdisk") {
							// If rdisk path fails, try disk path
							diskPath := strings.Replace(partitionPath, "/dev/rdisk", "/dev/disk", 1)
							mountPoint, mountErr = findMountPointForDevice(diskPath)
						} else if mountErr != nil && strings.Contains(partitionPath, "disk") && !strings.Contains(partitionPath, "rdisk") {
							// If disk path fails, try rdisk path
							rdiskPath := strings.Replace(partitionPath, "/dev/disk", "/dev/rdisk", 1)
							mountPoint, mountErr = findMountPointForDevice(rdiskPath)
						}
						var mountInfo string
						var mounted bool
						if mountErr != nil {
							mounted = false
							mountInfo = ""
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

						partitions = append(partitions, PartitionInfo{
							Number:       partNum,
							Name:         fmt.Sprintf("%s%d", diskDevice, partNum),
							Type:         fmt.Sprintf("0x%02x", logicalPart.Type),
							FileSystem:   fsType,
							Size:         formatBytes(logicalPart.Sectors * uint32(sectorSize)),
							FirstLBA:     uint64(logicalPart.FirstSector),
							TotalSectors: uint64(logicalPart.Sectors),
							SectorSize:   sectorSize,
							MountPoint:   mountPoint,
							MountInfo:    mountInfo,
							Mounted:      mounted,
						})
						partNum++
					}
				}
			} else {
				// Primary partition
				fsType := detectFileSystem(file, int64(part.FirstSector*uint32(sectorSize)))
				// Get mount point for this partition
				var partitionPath string
				if strings.Contains(diskDevice, "disk") || strings.Contains(diskDevice, "rdisk") {
					partitionPath = fmt.Sprintf("%ss%d", diskDevice, partNum)
				} else {
					partitionPath = fmt.Sprintf("%s%d", diskDevice, partNum)
				}
				// Try to find mount point - on macOS, try both disk and rdisk versions
				mountPoint, mountErr := findMountPointForDevice(partitionPath)
				if mountErr != nil && strings.Contains(partitionPath, "rdisk") {
					// If rdisk path fails, try disk path
					diskPath := strings.Replace(partitionPath, "/dev/rdisk", "/dev/disk", 1)
					mountPoint, mountErr = findMountPointForDevice(diskPath)
				} else if mountErr != nil && strings.Contains(partitionPath, "disk") && !strings.Contains(partitionPath, "rdisk") {
					// If disk path fails, try rdisk path
					rdiskPath := strings.Replace(partitionPath, "/dev/disk", "/dev/rdisk", 1)
					mountPoint, mountErr = findMountPointForDevice(rdiskPath)
				}
				var mountInfo string
				var mounted bool
				if mountErr != nil {
					mounted = false
					mountInfo = ""
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

				partitions = append(partitions, PartitionInfo{
					Number:       partNum,
					Name:         fmt.Sprintf("%s%d", diskDevice, partNum),
					Type:         fmt.Sprintf("0x%02x", part.Type),
					FileSystem:   fsType,
					Size:         formatBytes(part.Sectors * uint32(sectorSize)),
					FirstLBA:     uint64(part.FirstSector),
					TotalSectors: uint64(part.Sectors),
					SectorSize:   sectorSize,
					MountPoint:   mountPoint,
					MountInfo:    mountInfo,
					Mounted:      mounted,
				})
				partNum++
			}
		}
	}

	// Calculate unused space at the end
	// Get total disk size
	var totalDiskSectors uint64
	if stat, err := file.Stat(); err == nil {
		totalDiskSectors = uint64(stat.Size()) / sectorSize
	} else {
		if size, err := getBlockDeviceSize(diskDevice); err == nil {
			totalDiskSectors = uint64(size) / sectorSize
		}
	}

	// Find the last partition's end
	var lastPartitionEnd uint64
	for _, part := range partitions {
		// Calculate end LBA (LastLBA might be 0, so calculate from FirstLBA + TotalSectors)
		partEnd := part.FirstLBA + part.TotalSectors - 1
		if part.LastLBA > 0 && part.LastLBA > partEnd {
			partEnd = part.LastLBA
		}
		if partEnd > lastPartitionEnd {
			lastPartitionEnd = partEnd
		}
	}

	// Add unused space if there's any
	// Handle case where there are no partitions (lastPartitionEnd = 0)
	// MBR typically starts partitions at LBA 1
	var unusedStart uint64
	if len(partitions) == 0 {
		// No partitions - start from LBA 1
		unusedStart = 1
	} else {
		unusedStart = lastPartitionEnd + 1
	}

	// Always try to add unused space if we have disk size info
	// If no partitions and we can't get size, try harder to get it
	if len(partitions) == 0 {
		if totalDiskSectors == 0 {
			// Try alternative method to get disk size
			if size, err := getBlockDeviceSize(diskDevice); err == nil {
				totalDiskSectors = uint64(size) / sectorSize
			}
		}

		if totalDiskSectors > 0 && unusedStart < totalDiskSectors {
			unusedSectors := totalDiskSectors - unusedStart
			partitions = append(partitions, PartitionInfo{
				Number:       1,
				Name:         "Unused",
				Type:         "  ",
				FileSystem:   "",
				Size:         formatBytes(unusedSectors * sectorSize),
				FirstLBA:     unusedStart,
				LastLBA:      unusedStart + unusedSectors - 1,
				TotalSectors: unusedSectors,
				SectorSize:   sectorSize,
				Unused:       true,
			})
		} else {
			// Still show unused space even if we can't get exact size
			// Use reasonable placeholder values that won't cause CHS calculation issues
			// Assume a minimum 1GB disk for display purposes
			placeholderSectors := uint64(1024 * 1024 * 1024 / 512) // 1GB in sectors
			partitions = append(partitions, PartitionInfo{
				Number:       1,
				Name:         "Unused",
				Type:         "  ",
				FileSystem:   "",
				Size:         "Unknown",
				FirstLBA:     unusedStart,
				LastLBA:      unusedStart + placeholderSectors - 1,
				TotalSectors: placeholderSectors,
				SectorSize:   sectorSize,
				Unused:       true,
			})
		}
	} else if totalDiskSectors > 0 && unusedStart < totalDiskSectors {
		unusedSectors := totalDiskSectors - unusedStart
		if unusedSectors > 0 {
			partitions = append(partitions, PartitionInfo{
				Number:       len(partitions) + 1,
				Name:         "Unused",
				Type:         "  ",
				FileSystem:   "",
				Size:         formatBytes(unusedSectors * sectorSize),
				FirstLBA:     unusedStart,
				LastLBA:      unusedStart + unusedSectors - 1,
				TotalSectors: unusedSectors,
				SectorSize:   sectorSize,
				Unused:       true,
			})
		}
	}

	return partitions, nil
}

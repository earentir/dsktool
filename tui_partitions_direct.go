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
	if isGPTDiskSafe(file) {
		return getGPTPartitionsData(file, diskPath, sectorSize)
	}
	return getMBRPartitionsData(file, diskPath, sectorSize)
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
		if part.LastLBA > lastPartitionEnd {
			lastPartitionEnd = part.LastLBA
		}
	}

	// Add unused space if there's any
	if totalDiskSectors > 0 && lastPartitionEnd > 0 && lastPartitionEnd < totalDiskSectors-1 {
		unusedStart := lastPartitionEnd + 1
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
		partEnd := part.FirstLBA + part.TotalSectors - 1
		if partEnd > lastPartitionEnd {
			lastPartitionEnd = partEnd
		}
	}

	// Add unused space if there's any
	if totalDiskSectors > 0 && lastPartitionEnd > 0 && lastPartitionEnd < totalDiskSectors-1 {
		unusedStart := lastPartitionEnd + 1
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

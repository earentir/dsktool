package main

import (
	"fmt"
	"strings"
)

// PartitionInfo represents information about a partition for TUI display
type PartitionInfo struct {
	Number       int
	Name         string
	Type         string
	FileSystem   string
	Size         string
	FirstLBA     uint64
	LastLBA      uint64
	TotalSectors uint64
	SectorSize   uint64
	TypeGUID     string // For GPT
	UniqueGUID   string // For GPT
	Status       string // For MBR (Active/Inactive)
	MountPoint   string // Mount point if mounted
	MountInfo    string // Full mount info with filesystem stats
	Mounted      bool   // Whether partition is mounted
}

// getPartitionsData returns structured partition information
func getPartitionsData(diskPath string) ([]PartitionInfo, error) {
	// Try to get structured data directly
	partitions, err := getPartitionsDataDirect(diskPath)
	if err == nil && len(partitions) > 0 {
		return partitions, nil
	}

	// Fallback to parsing text output
	output, err := listPartitionsSafe(diskPath)
	if err != nil {
		return nil, err
	}

	// Parse the output into structured data
	return parsePartitionOutput(output), nil
}

// getPartitionsDataDirect gets partition data directly without parsing text
func getPartitionsDataDirect(diskPath string) ([]PartitionInfo, error) {
	return getPartitionsDataDirectPlatform(diskPath)
}

// parsePartitionOutput parses the partition output string into structured data
func parsePartitionOutput(output string) []PartitionInfo {
	// First try to parse MBR format (simpler, single-line per partition)
	partitions := parseMBRPartitionOutput(output)
	if len(partitions) > 0 {
		return partitions
	}

	// Otherwise parse GPT format (multi-line per partition)
	return parseGPTPartitionOutput(output)
}

// parseGPTPartitionOutput parses GPT partition output format
func parseGPTPartitionOutput(output string) []PartitionInfo {
	var partitions []PartitionInfo
	lines := strings.Split(output, "\n")

	currentPartition := PartitionInfo{}
	inPartition := false
	partNum := 0

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Check if this is a partition entry start (GPT format)
		if strings.Contains(line, "Partition Name :") {
			if inPartition {
				// Save previous partition
				if partNum > 0 {
					currentPartition.Number = partNum
					partitions = append(partitions, currentPartition)
				}
			}
			inPartition = true
			partNum++
			currentPartition = PartitionInfo{Number: partNum}

			// Extract partition name
			if idx := strings.Index(line, ":"); idx != -1 {
				currentPartition.Name = strings.TrimSpace(line[idx+1:])
			}
		} else if inPartition {
			if strings.Contains(line, "FileSystem     :") {
				if idx := strings.Index(line, ":"); idx != -1 {
					currentPartition.FileSystem = strings.TrimSpace(line[idx+1:])
				}
			} else if strings.Contains(line, "Total Size     :") {
				if idx := strings.Index(line, ":"); idx != -1 {
					currentPartition.Size = strings.TrimSpace(line[idx+1:])
				}
			} else if strings.Contains(line, "TypeGUID       :") {
				if idx := strings.Index(line, ":"); idx != -1 {
					currentPartition.TypeGUID = strings.TrimSpace(line[idx+1:])
					// Use TypeGUID as Type for display
					currentPartition.Type = currentPartition.TypeGUID
				}
			} else if strings.Contains(line, "UniqueGUID     :") {
				if idx := strings.Index(line, ":"); idx != -1 {
					currentPartition.UniqueGUID = strings.TrimSpace(line[idx+1:])
				}
			} else if strings.Contains(line, "Sector Size    :") {
				if idx := strings.Index(line, ":"); idx != -1 {
					var sectorSize uint64
					fmt.Sscanf(strings.TrimSpace(line[idx+1:]), "%d", &sectorSize)
					currentPartition.SectorSize = sectorSize
				}
			} else if strings.Contains(line, "FirstLBA       :") {
				if idx := strings.Index(line, ":"); idx != -1 {
					fmt.Sscanf(strings.TrimSpace(line[idx+1:]), "%d", &currentPartition.FirstLBA)
				}
			} else if strings.Contains(line, "LastLBA        :") {
				if idx := strings.Index(line, ":"); idx != -1 {
					fmt.Sscanf(strings.TrimSpace(line[idx+1:]), "%d", &currentPartition.LastLBA)
				}
			} else if strings.Contains(line, "Total Sectors  :") {
				if idx := strings.Index(line, ":"); idx != -1 {
					fmt.Sscanf(strings.TrimSpace(line[idx+1:]), "%d", &currentPartition.TotalSectors)
				}
			}
		}
	}

	// Add last partition if we were in one
	if inPartition && partNum > 0 {
		partitions = append(partitions, currentPartition)
	}

	return partitions
}

// parseMBRPartitionOutput parses MBR partition output format
func parseMBRPartitionOutput(output string) []PartitionInfo {
	var partitions []PartitionInfo
	lines := strings.Split(output, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "  ") && strings.Contains(line, "Type:") && !strings.Contains(line, "Extended") {
			// Primary partition: "  1. Type: 0x02, FirstSector: 2048, Sectors: 4194304, FileSystem: ext4, SectorSize: 512 bytes, Total: 2.0 GB"
			var partNum int
			var partType string
			var firstSector uint32
			var fsType, size string

			fmt.Sscanf(line, "  %d. Type: %s", &partNum, &partType)

			// Extract other fields manually
			var sectors uint32
			var sectorSizeVal uint64
			if idx := strings.Index(line, "FirstSector: "); idx != -1 {
				fmt.Sscanf(line[idx+13:], "%d", &firstSector)
			}
			if idx := strings.Index(line, "Sectors: "); idx != -1 {
				fmt.Sscanf(line[idx+9:], "%d", &sectors)
			}
			if idx := strings.Index(line, "SectorSize: "); idx != -1 {
				fmt.Sscanf(line[idx+12:], "%d", &sectorSizeVal)
			}
			if idx := strings.Index(line, "FileSystem: "); idx != -1 {
				fsPart := line[idx+12:]
				if endIdx := strings.Index(fsPart, ","); endIdx != -1 {
					fsType = strings.TrimSpace(fsPart[:endIdx])
				} else {
					fsType = strings.TrimSpace(fsPart)
				}
			}
			if idx := strings.Index(line, "Total: "); idx != -1 {
				size = strings.TrimSpace(line[idx+7:])
			}

			partitions = append(partitions, PartitionInfo{
				Number:       partNum,
				Name:         fmt.Sprintf("Partition %d", partNum),
				Type:         partType,
				FileSystem:   fsType,
				Size:         size,
				FirstLBA:     uint64(firstSector),
				TotalSectors: uint64(sectors),
				SectorSize:   sectorSizeVal,
			})
		} else if strings.HasPrefix(line, "    ") && strings.Contains(line, "Type:") {
			// Logical partition: "    2. Type: 0x83 (Logical), FirstSector: 4096, ..."
			var partNum int
			var partType string
			var firstSector uint32
			var sectors uint32
			var sectorSizeVal uint64
			var fsType, size string

			fmt.Sscanf(line, "    %d. Type: %s", &partNum, &partType)

			if idx := strings.Index(line, "FirstSector: "); idx != -1 {
				fmt.Sscanf(line[idx+13:], "%d", &firstSector)
			}
			if idx := strings.Index(line, "Sectors: "); idx != -1 {
				fmt.Sscanf(line[idx+9:], "%d", &sectors)
			}
			if idx := strings.Index(line, "SectorSize: "); idx != -1 {
				fmt.Sscanf(line[idx+12:], "%d", &sectorSizeVal)
			}
			if idx := strings.Index(line, "FileSystem: "); idx != -1 {
				fsPart := line[idx+12:]
				if endIdx := strings.Index(fsPart, ","); endIdx != -1 {
					fsType = strings.TrimSpace(fsPart[:endIdx])
				} else {
					fsType = strings.TrimSpace(fsPart)
				}
			}
			if idx := strings.Index(line, "Total: "); idx != -1 {
				size = strings.TrimSpace(line[idx+7:])
			}

			partitions = append(partitions, PartitionInfo{
				Number:       partNum,
				Name:         fmt.Sprintf("Partition %d", partNum),
				Type:         partType,
				FileSystem:   fsType,
				Size:         size,
				FirstLBA:     uint64(firstSector),
				TotalSectors: uint64(sectors),
				SectorSize:   sectorSizeVal,
			})
		}
	}

	return partitions
}

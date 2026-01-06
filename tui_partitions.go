//go:build darwin || linux

package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"strings"
	"text/template"
)

// listPartitionsSafe is a version of listPartitions that returns errors instead of calling log.Fatalf
func listPartitionsSafe(diskDevice string) (string, error) {
	var output strings.Builder

	file, err := os.Open(diskDevice)
	if err != nil {
		return "", fmt.Errorf("error opening disk: %w", err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("error stating disk: %w", err)
	}

	mode := info.Mode()
	if (mode & os.ModeDevice) == 0 {
		return "", fmt.Errorf("%s is not a device file", diskDevice)
	}

	// Get sector size
	sectorSize := uint64(getSectorSize(file))

	// Check if GPT or MBR
	if isGPTDiskSafe(file) {
		return listGPTPartitionsSafe(file, diskDevice, sectorSize, &output)
	}
	return listMBRPartitionsSafe(file, diskDevice, sectorSize, &output)
}

func isGPTDiskSafe(file *os.File) bool {
	_, err := file.Seek(512, io.SeekStart)
	if err != nil {
		return false
	}

	header := gptHeader{}
	err = binary.Read(file, binary.LittleEndian, &header)
	if err != nil {
		return false
	}

	return string(header.Signature[:]) == "EFI PART"
}

func listGPTPartitionsSafe(file *os.File, diskDevice string, sectorSize uint64, output *strings.Builder) (string, error) {
	headerBytes := make([]byte, 512)
	_, err := file.ReadAt(headerBytes, 512)
	if err != nil {
		return "", fmt.Errorf("error reading GPT header: %w", err)
	}

	header := gptHeader{}
	err = binary.Read(bytes.NewReader(headerBytes), binary.LittleEndian, &header)
	if err != nil {
		return "", fmt.Errorf("error parsing GPT header: %w", err)
	}

	if header.HeaderSize < 92 || int(header.HeaderSize) > len(headerBytes) {
		return "", fmt.Errorf("invalid GPT header size: %d", header.HeaderSize)
	}

	tableBytes := uint64(header.NumPartEntries) * uint64(header.PartEntrySize)
	table := make([]byte, tableBytes)
	_, err = file.ReadAt(table, int64(header.PartitionEntryLBA*512))
	if err != nil {
		return "", fmt.Errorf("error reading GPT entries: %w", err)
	}

	partitions := make([]gptPartition, 0, header.NumPartEntries)
	for i := uint32(0); i < header.NumPartEntries; i++ {
		off := uint64(i) * uint64(header.PartEntrySize)
		partition := gptPartition{}
		err := binary.Read(bytes.NewReader(table[off:off+uint64(header.PartEntrySize)]), binary.LittleEndian, &partition)
		if err != nil {
			return "", fmt.Errorf("error reading partition entry: %w", err)
		}
		if !isAllZero(partition.TypeGUID[:]) {
			partitions = append(partitions, partition)
		}
	}

	tmpl, err := template.New("partition").Parse(partitionTmpl)
	if err != nil {
		return "", fmt.Errorf("error parsing partition template: %w", err)
	}

	var displayPartitions []gptPartitionDisplay
	var partID int
	for _, part := range partitions {
		if part.FirstLBA != 0 {
			partID++
			fsType := detectFileSystem(file, int64(part.FirstLBA*uint64(sectorSize)))
			totalSectors := part.LastLBA - part.FirstLBA + 1

			partName := decodeUTF16LE(part.PartitionName[:])
			typeGUID := guidToString(part.TypeGUID[:])
			uniqueGUID := guidToString(part.UniqueGUID[:])

			displayPartitions = append(displayPartitions, gptPartitionDisplay{
				Disk:          diskDevice,
				DiskType:      "GPT",
				Partition:     part,
				PartitionName: fmt.Sprintf("%s%d", diskDevice, partID),
				Name:          partName,
				Filesystem:    fsType,
				TotalSectors:  totalSectors,
				SectorSize:    sectorSize,
				Total:         formatBytes(totalSectors * sectorSize),
				TypeGUIDStr:   typeGUID,
				UniqueGUIDStr: uniqueGUID,
			})
		}
	}

	for _, displayPartition := range displayPartitions {
		err = tmpl.Execute(output, displayPartition)
		if err != nil {
			return "", fmt.Errorf("error executing partition template: %w", err)
		}
	}

	return output.String(), nil
}

func listMBRPartitionsSafe(file *os.File, diskDevice string, sectorSize uint64, output *strings.Builder) (string, error) {
	_, err := file.Seek(0, io.SeekStart)
	if err != nil {
		return "", fmt.Errorf("error seeking to MBR: %w", err)
	}

	mbr := mbrStruct{}
	err = binary.Read(file, binary.LittleEndian, &mbr)
	if err != nil {
		return "", fmt.Errorf("error reading MBR: %w", err)
	}

	if mbr.Signature != 0xAA55 {
		return "", fmt.Errorf("invalid MBR signature (0x%04X)", mbr.Signature)
	}

	var sizeBytes int64
	if stat, err := file.Stat(); err == nil {
		sizeBytes = stat.Size()
	}
	if sizeBytes <= 0 {
		// Try to get size, but don't fail if we can't
		if size, err := getBlockDeviceSize(diskDevice); err == nil {
			sizeBytes = size
		}
	}

	output.WriteString("Partitions:\n")
	partNum := 1
	for _, part := range mbr.Partitions {
		if part.Sectors != 0 {
			if isExtendedType(part.Type) {
				output.WriteString(fmt.Sprintf("  %d. Type: 0x%02x (Extended), FirstSector: %d, Sectors: %d, SectorSize: %d bytes, Total: %s\n",
					partNum, part.Type, part.FirstSector, part.Sectors, sectorSize, formatBytes(part.Sectors*uint32(sectorSize))))
				partNum++

				logicalParts, err := readEBRChain(file, sizeBytes, sectorSize, part.FirstSector)
				if err != nil {
					output.WriteString(fmt.Sprintf("    Warning: Could not read extended partition chain: %v\n", err))
				} else {
					for _, logicalPart := range logicalParts {
						fsType := detectFileSystem(file, int64(logicalPart.FirstSector*uint32(sectorSize)))
						output.WriteString(fmt.Sprintf("    %d. Type: 0x%02x (Logical), FirstSector: %d, Sectors: %d, FileSystem: %s, SectorSize: %d bytes, Total: %s\n",
							partNum, logicalPart.Type, logicalPart.FirstSector, logicalPart.Sectors, fsType, sectorSize, formatBytes(logicalPart.Sectors*uint32(sectorSize))))
						partNum++
					}
				}
			} else {
				fsType := detectFileSystem(file, int64(part.FirstSector*uint32(sectorSize)))
				output.WriteString(fmt.Sprintf("  %d. Type: 0x%02x, FirstSector: %d, Sectors: %d, FileSystem: %s, SectorSize: %d bytes, Total: %s\n",
					partNum, part.Type, part.FirstSector, part.Sectors, fsType, sectorSize, formatBytes(part.Sectors*uint32(sectorSize))))
				partNum++
			}
		}
	}

	return output.String(), nil
}

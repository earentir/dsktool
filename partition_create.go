//go:build darwin || linux

package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"os"
	"strings"
)

// createPartition creates a new partition on the disk
func createPartition(diskPath string, unusedPart PartitionInfo, fields []FormField) error {
	// Parse form data first to get scheme
	form := &PartitionCreateForm{
		fields:          fields,
		unusedPartition: unusedPart,
	}
	// Determine scheme from first field
	if len(fields) > 0 {
		form.isGPT = (fields[0].value == "GPT")
	}

	preview, err := parsePartitionForm(form)
	if err != nil {
		return err
	}

	isGPT := form.isGPT

	// Check if extended partition (MBR only) - type is "Extended Partition"
	isExtended := false
	if !isGPT {
		// MBR: type field is at index 5
		if len(fields) > 5 && fields[5].value == "Extended Partition" {
			isExtended = true
		}
	}

	// On macOS, use raw device (rdisk) for write operations
	writePath := diskPath
	if strings.HasPrefix(diskPath, "/dev/disk") && !strings.HasPrefix(diskPath, "/dev/rdisk") {
		writePath = strings.Replace(diskPath, "/dev/disk", "/dev/rdisk", 1)
	}

	file, err := os.OpenFile(writePath, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("error opening disk for writing (tried %s): %w", writePath, err)
	}
	defer file.Close()

	// Check if GPT or MBR
	if isGPTDiskSafe(file) {
		return createGPTPartition(file, diskPath, preview, fields)
	}
	return createMBRPartition(file, diskPath, preview, fields, isExtended)
}

// createGPTPartition creates a GPT partition
func createGPTPartition(file *os.File, diskDevice string, preview PartitionInfo, fields []FormField) error {
	// Read GPT header
	headerBytes := make([]byte, 512)
	_, err := file.ReadAt(headerBytes, 512)
	if err != nil {
		return fmt.Errorf("error reading GPT header: %w", err)
	}

	header := gptHeader{}
	err = binary.Read(bytes.NewReader(headerBytes), binary.LittleEndian, &header)
	if err != nil {
		return fmt.Errorf("error parsing GPT header: %w", err)
	}

	// Read partition table
	tableBytes := uint64(header.NumPartEntries) * uint64(header.PartEntrySize)
	table := make([]byte, tableBytes)
	_, err = file.ReadAt(table, int64(header.PartitionEntryLBA*512))
	if err != nil {
		return fmt.Errorf("error reading GPT entries: %w", err)
	}

	// Find first empty slot or use specified partition number
	requestedPartNum := preview.Number
	var emptySlot uint32 = ^uint32(0) // Invalid slot

	// First, check if requested partition number slot is available
	if requestedPartNum > 0 {
		// Count existing partitions to find slot index
		partCount := uint32(0)
		for i := uint32(0); i < header.NumPartEntries; i++ {
			off := uint64(i) * uint64(header.PartEntrySize)
			partition := gptPartition{}
			err := binary.Read(bytes.NewReader(table[off:off+uint64(header.PartEntrySize)]), binary.LittleEndian, &partition)
			if err != nil {
				continue
			}
			if !isAllZero(partition.TypeGUID[:]) && partition.FirstLBA != 0 {
				partCount++
				if partCount == uint32(requestedPartNum-1) {
					// This slot corresponds to the requested partition number
					emptySlot = i
					break
				}
			}
		}
		// If not found by number, find first truly empty slot
		if emptySlot == ^uint32(0) {
			for i := uint32(0); i < header.NumPartEntries; i++ {
				off := uint64(i) * uint64(header.PartEntrySize)
				partition := gptPartition{}
				err := binary.Read(bytes.NewReader(table[off:off+uint64(header.PartEntrySize)]), binary.LittleEndian, &partition)
				if err != nil {
					continue
				}
				if isAllZero(partition.TypeGUID[:]) || partition.FirstLBA == 0 {
					emptySlot = i
					break
				}
			}
		}
	} else {
		// Find first empty slot
		for i := uint32(0); i < header.NumPartEntries; i++ {
			off := uint64(i) * uint64(header.PartEntrySize)
			partition := gptPartition{}
			err := binary.Read(bytes.NewReader(table[off:off+uint64(header.PartEntrySize)]), binary.LittleEndian, &partition)
			if err != nil {
				continue
			}

			// Check if entry is empty
			if isAllZero(partition.TypeGUID[:]) || partition.FirstLBA == 0 {
				emptySlot = i
				break
			}
		}
	}

	if emptySlot == ^uint32(0) {
		return fmt.Errorf("no free partition slot available")
	}

	// Create partition entry
	partition := gptPartition{}

	// Set TypeGUID based on type field (GPT: field index 5)
	typeGUID := getTypeGUIDFromName(fields[5].value)
	copy(partition.TypeGUID[:], typeGUID)

	// Generate UniqueGUID (simplified - in production should use proper GUID generation)
	uniqueGUID := generateGUID()
	copy(partition.UniqueGUID[:], uniqueGUID)

	partition.FirstLBA = preview.FirstLBA
	partition.LastLBA = preview.LastLBA
	partition.AttributeFlags = 0

	// Set partition name (GPT: field index 2)
	nameBytes := encodeUTF16LE(fields[2].value)
	copy(partition.PartitionName[:], nameBytes)
	if len(nameBytes) < len(partition.PartitionName) {
		// Zero out remaining bytes
		for i := len(nameBytes); i < len(partition.PartitionName); i++ {
			partition.PartitionName[i] = 0
		}
	}

	// Write partition entry to table
	off := uint64(emptySlot) * uint64(header.PartEntrySize)
	var buf bytes.Buffer
	err = binary.Write(&buf, binary.LittleEndian, partition)
	if err != nil {
		return fmt.Errorf("error encoding partition: %w", err)
	}
	copy(table[off:off+uint64(header.PartEntrySize)], buf.Bytes())

	// Calculate new partition array CRC32
	newPartArrayCRC := crc32.ChecksumIEEE(table)
	header.PartEntryArrayCRC32 = newPartArrayCRC

	// Update header CRC32
	headerBytes = make([]byte, 512)
	_, err = file.ReadAt(headerBytes, 512)
	if err != nil {
		return fmt.Errorf("error reading header for CRC: %w", err)
	}

	// Update partition array CRC32 in header bytes (offset 88)
	binary.LittleEndian.PutUint32(headerBytes[88:92], newPartArrayCRC)

	// Zero CRC field for calculation
	for i := 16; i < 20; i++ {
		headerBytes[i] = 0
	}

	// Calculate new header CRC32
	newHeaderCRC := crc32.ChecksumIEEE(headerBytes[:header.HeaderSize])
	binary.LittleEndian.PutUint32(headerBytes[16:20], newHeaderCRC)

	// Write updated partition table (primary)
	_, err = file.WriteAt(table, int64(header.PartitionEntryLBA*512))
	if err != nil {
		return fmt.Errorf("error writing GPT entries: %w", err)
	}

	// Write updated header (primary)
	_, err = file.WriteAt(headerBytes[:header.HeaderSize], 512)
	if err != nil {
		return fmt.Errorf("error writing GPT header: %w", err)
	}

	// Update backup GPT header and table
	backupHeaderBytes := make([]byte, 512)
	_, err = file.ReadAt(backupHeaderBytes, int64(header.BackupLBA*512))
	if err != nil {
		return fmt.Errorf("error reading backup GPT header: %w", err)
	}

	backupHeader := gptHeader{}
	err = binary.Read(bytes.NewReader(backupHeaderBytes), binary.LittleEndian, &backupHeader)
	if err != nil {
		return fmt.Errorf("error parsing backup GPT header: %w", err)
	}

	// Update backup header with new partition array CRC32
	binary.LittleEndian.PutUint32(backupHeaderBytes[88:92], newPartArrayCRC)

	// Zero CRC field for calculation
	for i := 16; i < 20; i++ {
		backupHeaderBytes[i] = 0
	}

	// Calculate backup header CRC32
	backupHeaderCRC := crc32.ChecksumIEEE(backupHeaderBytes[:backupHeader.HeaderSize])
	binary.LittleEndian.PutUint32(backupHeaderBytes[16:20], backupHeaderCRC)

	// Write backup partition table
	backupTableLBA := backupHeader.PartitionEntryLBA
	_, err = file.WriteAt(table, int64(backupTableLBA*512))
	if err != nil {
		return fmt.Errorf("error writing backup GPT entries: %w", err)
	}

	// Write backup header
	_, err = file.WriteAt(backupHeaderBytes[:backupHeader.HeaderSize], int64(header.BackupLBA*512))
	if err != nil {
		return fmt.Errorf("error writing backup GPT header: %w", err)
	}

	return nil
}

// createMBRPartition creates an MBR partition
func createMBRPartition(file *os.File, diskDevice string, preview PartitionInfo, fields []FormField, isExtended bool) error {
	// Read MBR
	mbr := mbrStruct{}
	_, err := file.Seek(0, 0)
	if err != nil {
		return fmt.Errorf("error seeking to MBR: %w", err)
	}

	err = binary.Read(file, binary.LittleEndian, &mbr)
	if err != nil {
		return fmt.Errorf("error reading MBR: %w", err)
	}

	if mbr.Signature != 0xAA55 {
		return fmt.Errorf("invalid MBR signature")
	}

	// Find slot for requested partition number or first empty slot
	requestedPartNum := preview.Number
	emptySlot := -1

	if requestedPartNum > 0 && requestedPartNum <= 4 {
		// Try to use the requested slot (partition numbers are 1-indexed, slots are 0-indexed)
		slotIdx := requestedPartNum - 1
		if mbr.Partitions[slotIdx].Sectors == 0 {
			emptySlot = slotIdx
		}
	}

	// If requested slot not available, find first empty slot
	if emptySlot == -1 {
		for i := 0; i < 4; i++ {
			if mbr.Partitions[i].Sectors == 0 {
				emptySlot = i
				break
			}
		}
	}

	if emptySlot == -1 {
		return fmt.Errorf("no free partition slot available (MBR supports max 4 primary partitions)")
	}

	// Create partition entry
	part := mbrPartition{}
	part.Status = 0x00 // Not bootable
	// MBR: type field is at index 5
	part.Type = getMBRTypeFromName(fields[5].value)
	part.FirstSector = uint32(preview.FirstLBA)
	part.Sectors = uint32(preview.TotalSectors)

	mbr.Partitions[emptySlot] = part

	// Write back MBR
	_, err = file.Seek(0, 0)
	if err != nil {
		return fmt.Errorf("error seeking to MBR: %w", err)
	}

	err = binary.Write(file, binary.LittleEndian, &mbr)
	if err != nil {
		return fmt.Errorf("error writing MBR: %w", err)
	}

	return nil
}

// Helper functions for GUID and type conversion
func getTypeGUIDFromName(typeName string) []byte {
	// Common GPT partition type GUIDs
	guidMap := map[string]string{
		"Linux Filesystem":    "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
		"Linux Swap":          "0657FD6D-A4AB-43C4-84E5-0933C84B4F4F",
		"EFI System":          "C12A7328-F81F-11D2-BA4B-00A0C93EC93B",
		"Windows Basic Data":  "EBD0A0A2-B9E5-4433-87C0-68B6B72699C7",
		"Microsoft Reserved":  "E3C9E316-0B5C-4DB8-817D-F92DF00215AE",
	}

	guidStr, ok := guidMap[typeName]
	if !ok {
		// Default to Linux Filesystem
		guidStr = guidMap["Linux Filesystem"]
	}

	return parseGUID(guidStr)
}

func getMBRTypeFromName(typeName string) byte {
	// Common MBR partition types
	typeMap := map[string]byte{
		"Linux Filesystem":   0x83, // Linux
		"Linux Swap":         0x82, // Linux swap
		"EFI System":         0xEF, // EFI System Partition
		"Windows Basic Data": 0x07, // NTFS/exFAT
		"Microsoft Reserved": 0x27, // Windows RE
		"Extended Partition": 0x05, // Extended partition (shouldn't normally be called here)
	}

	partType, ok := typeMap[typeName]
	if !ok {
		// Default to Linux
		return 0x83
	}

	return partType
}

func parseGUID(guidStr string) []byte {
	// Parse GUID string like "0FC63DAF-8483-4772-8E79-3D69D8477DE4" into 16 bytes
	guidStr = strings.ReplaceAll(guidStr, "-", "")
	if len(guidStr) != 32 {
		// Return default Linux Filesystem GUID
		return parseGUID("0FC63DAF-8483-4772-8E79-3D69D8477DE4")
	}

	guid := make([]byte, 16)
	for i := 0; i < 16; i++ {
		var b byte
		fmt.Sscanf(guidStr[i*2:i*2+2], "%02x", &b)
		guid[i] = b
	}

	return guid
}

func generateGUID() []byte {
	// Simplified GUID generation - in production should use proper random GUID
	// For now, return a placeholder
	return []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F}
}

func encodeUTF16LE(s string) []byte {
	// Simple UTF-16 LE encoding (simplified - doesn't handle all Unicode)
	result := make([]byte, 0, len(s)*2)
	for _, r := range s {
		result = append(result, byte(r&0xFF), byte(r>>8))
	}
	return result
}

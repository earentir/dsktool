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

// deletePartition deletes a partition from the disk
func deletePartition(diskPath string, part PartitionInfo) error {
	// Check if the partition is mounted
	if part.Mounted {
		return fmt.Errorf("cannot delete mounted partition. Please unmount it first: %s", part.MountPoint)
	}

	// On macOS, use raw device (rdisk) for write operations
	writePath := diskPath
	if strings.HasPrefix(diskPath, "/dev/disk") && !strings.HasPrefix(diskPath, "/dev/rdisk") {
		writePath = strings.Replace(diskPath, "/dev/disk", "/dev/rdisk", 1)
	}

	file, err := os.OpenFile(writePath, os.O_RDWR, 0)
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "resource busy") || strings.Contains(errStr, "device busy") {
			return fmt.Errorf("device is busy (may be mounted). Please unmount all partitions first: %w", err)
		}
		return fmt.Errorf("error opening disk for writing (tried %s): %w", writePath, err)
	}
	defer file.Close()

	// Check if GPT or MBR
	if isGPTDiskSafe(file) {
		return deleteGPTPartition(file, diskPath, part)
	}
	return deleteMBRPartition(file, diskPath, part)
}

// deleteGPTPartition deletes a GPT partition
func deleteGPTPartition(file *os.File, diskDevice string, part PartitionInfo) error {
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

	// Find and zero out the partition entry
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
		if partID == part.Number {
			// Found the partition - zero it out
			zeroEntry := make([]byte, header.PartEntrySize)
			copy(table[off:off+uint64(header.PartEntrySize)], zeroEntry)

			// Calculate new partition array CRC32
			newPartArrayCRC := crc32.ChecksumIEEE(table)
			header.PartEntryArrayCRC32 = newPartArrayCRC

			// Update header CRC32 (must zero CRC field first, then calculate)
			// Read current header bytes to preserve all fields
			headerBytes = make([]byte, 512)
			_, err = file.ReadAt(headerBytes, 512)
			if err != nil {
				return fmt.Errorf("error reading header for CRC: %w", err)
			}

			// Update the partition array CRC32 in the header bytes (offset 88)
			binary.LittleEndian.PutUint32(headerBytes[88:92], newPartArrayCRC)

			// Zero the CRC field (bytes 16-19) for calculation
			for i := 16; i < 20; i++ {
				headerBytes[i] = 0
			}

			// Calculate new header CRC32
			newHeaderCRC := crc32.ChecksumIEEE(headerBytes[:header.HeaderSize])

			// Update CRC32 in header bytes
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
			// Read backup header
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

			// Update backup header with new partition array CRC32 (offset 88)
			binary.LittleEndian.PutUint32(backupHeaderBytes[88:92], newPartArrayCRC)

			// Zero CRC field for calculation
			for i := 16; i < 20; i++ {
				backupHeaderBytes[i] = 0
			}

			// Calculate backup header CRC32
			backupHeaderCRC := crc32.ChecksumIEEE(backupHeaderBytes[:backupHeader.HeaderSize])

			// Update CRC32 in backup header bytes
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
	}

	return fmt.Errorf("partition not found")
}

// deleteMBRPartition deletes an MBR partition
func deleteMBRPartition(file *os.File, diskDevice string, part PartitionInfo) error {
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

	// Find and zero out the partition entry
	partID := 0
	for i := 0; i < 4; i++ {
		if mbr.Partitions[i].Sectors != 0 {
			partID++
			if partID == part.Number {
				// Found the partition - zero it out
				mbr.Partitions[i] = mbrPartition{}

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

			// TODO: Handle extended partition deletion
			// Extended partitions are more complex as they involve EBR chain
		}
	}

	return fmt.Errorf("partition not found")
}

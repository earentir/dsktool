//go:build darwin

package main

import (
	"bytes"
	"encoding/binary"
	"io"
	"os"
	"strings"
	"syscall"
	"unsafe"
)


// getDiskType detects the type of disk (physical, synthesized, image, etc.)
func getDiskType(devPath string) string {
	// Extract disk number from path (e.g., "/dev/disk3" -> "3")
	diskName := strings.TrimPrefix(devPath, "/dev/disk")

	// Check if it's a whole disk (physical) or a partition
	// Whole disks typically don't have 's' in their name
	isWholeDisk := !strings.Contains(diskName, "s")

	if !isWholeDisk {
		return "partition"
	}

	// Try to open the device to check properties
	file, err := os.Open(devPath)
	if err != nil {
		// If we can't open it, try raw device
		rawPath := strings.Replace(devPath, "/dev/disk", "/dev/rdisk", 1)
		file, err = os.Open(rawPath)
		if err != nil {
			return "unknown"
		}
	}
	defer file.Close()

	// Check if it's likely a synthesized disk by checking if it's an APFS container
	// Synthesized disks are typically APFS containers
	if isAPFSContainer(devPath) {
		return "synthesized"
	}

	// Check if we can get physical block size - physical disks usually support this
	var physBlockSize uint32
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), DKIOCGETPHYSICALBLOCKSIZE, uintptr(unsafe.Pointer(&physBlockSize)))

	// Check if it might be a disk image
	// Disk images often can't get physical block size or have different characteristics
	if errno != 0 {
		// Try to check if it's a disk image
		// One heuristic: disk images often don't support certain ioctls
		// Also check if it has no partition table or unusual characteristics
		if isLikelyDiskImage(devPath, file) {
			return "image"
		}
		// If we can't determine, might still be physical but with restrictions
		return "physical"
	}

	// If we can get physical block size, it's likely a physical disk
	// But also check if it has a partition table - images might not
	_, err = file.Seek(0, io.SeekStart)
	if err == nil {
		// Try to read MBR signature
		buf := make([]byte, 512)
		_, err = file.ReadAt(buf, 0)
		if err == nil {
			// Check for MBR signature (0x55AA at offset 510)
			if len(buf) >= 512 && buf[510] == 0x55 && buf[511] == 0xAA {
				return "physical"
			}
			// Check for GPT signature
			_, err = file.Seek(512, io.SeekStart)
			if err == nil {
				gptBuf := make([]byte, 8)
				_, err = file.ReadAt(gptBuf, 512)
				if err == nil && string(gptBuf) == "EFI PART" {
					return "physical"
				}
			}
		}
	}

	// Default to physical if we can get block size
	return "physical"
}

// isAPFSContainer checks if a disk is likely an APFS container (synthesized)
func isAPFSContainer(devPath string) bool {
	// APFS containers are typically whole disks that contain APFS volumes
	// We can check by trying to read the partition table
	// If it's GPT and has APFS partition types, it's likely a container
	file, err := os.Open(devPath)
	if err != nil {
		return false
	}
	defer file.Close()

	// Check if it's GPT by reading the signature
	_, err = file.Seek(512, io.SeekStart)
	if err != nil {
		return false
	}

	header := gptHeader{}
	err = binary.Read(file, binary.LittleEndian, &header)
	if err != nil {
		return false
	}

	if string(header.Signature[:]) != "EFI PART" {
		return false
	}

	// Try to read GPT and check for APFS partition types
	// APFS partition type GUID: 7C3457EF-0000-11AA-AA11-00306543ECAC
	apfsGUID := [16]byte{0xEF, 0x57, 0x34, 0x7C, 0x00, 0x00, 0xAA, 0x11, 0xAA, 0x11, 0x00, 0x30, 0x65, 0x43, 0xEC, 0xAC}

	headerBytes := make([]byte, 512)
	_, err = file.ReadAt(headerBytes, 512)
	if err != nil {
		return false
	}

	err = binary.Read(bytes.NewReader(headerBytes), binary.LittleEndian, &header)
	if err != nil {
		return false
	}

	// Read partition entries
	tableBytes := uint64(header.NumPartEntries) * uint64(header.PartEntrySize)
	table := make([]byte, tableBytes)
	_, err = file.ReadAt(table, int64(header.PartitionEntryLBA*512))
	if err != nil {
		return false
	}

	// Check for APFS partition types
	for i := uint32(0); i < header.NumPartEntries; i++ {
		off := uint64(i) * uint64(header.PartEntrySize)
		if off+16 > uint64(len(table)) {
			break
		}
		partGUID := table[off : off+16]
		if bytes.Equal(partGUID, apfsGUID[:]) {
			return true
		}
	}

	return false
}

// isLikelyDiskImage checks if a disk is likely a disk image
func isLikelyDiskImage(_ string, file *os.File) bool {
	// Disk images often have specific characteristics:
	// 1. They might not support all ioctls (like DKIOCGETPHYSICALBLOCKSIZE)
	// 2. They might not have a valid partition table
	// 3. They're often mounted from files

	// Try to read the first sector to check for partition table
	buf := make([]byte, 512)
	_, err := file.ReadAt(buf, 0)
	if err != nil {
		return false
	}

	// Check for MBR signature
	hasMBR := len(buf) >= 512 && buf[510] == 0x55 && buf[511] == 0xAA

	// Check for GPT signature at offset 512
	gptBuf := make([]byte, 8)
	_, err = file.ReadAt(gptBuf, 512)
	hasGPT := err == nil && string(gptBuf) == "EFI PART"

	// If it has neither MBR nor GPT, it might be a disk image
	// Disk images sometimes don't have partition tables
	if !hasMBR && !hasGPT {
		return true
	}

	// Another heuristic: try to get block count
	// Disk images might fail this
	var blockCount uint64
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), DKIOCGETBLOCKCOUNT, uintptr(unsafe.Pointer(&blockCount)))
	if errno != 0 {
		// Can't get block count - might be an image
		return true
	}

	return false
}

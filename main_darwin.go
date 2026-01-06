//go:build darwin

package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"text/template"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	// DKIOCGETBLOCKSIZE is the ioctl to get block size on macOS
	DKIOCGETBLOCKSIZE = 0x40046418
	// DKIOCGETBLOCKCOUNT is the ioctl to get block count on macOS
	DKIOCGETBLOCKCOUNT = 0x40086419
	// DKIOCGETPHYSICALBLOCKSIZE is the ioctl to get physical block size
	DKIOCGETPHYSICALBLOCKSIZE = 0x4004641A
)

func printDiskBytes(diskDevice string, numOfBytes int, startIndex int64) {
	err := printFirstNBytes(diskDevice, numOfBytes, startIndex)
	if err != nil {
		fmt.Printf("Error reading %d bytes from index %d, error: %v\n", numOfBytes, startIndex, err)
	}
}

func listPartitions(diskDevice string) {
	var file *os.File
	var err error

	// Convert raw device to regular block device
	// Raw devices on macOS don't work well for partition table reading
	if strings.HasPrefix(diskDevice, "/dev/rdisk") {
		diskDevice = strings.Replace(diskDevice, "/dev/rdisk", "/dev/disk", 1)
		fmt.Printf("Note: Using regular block device %s instead of raw device for partition reading\n", diskDevice)
	}

	// Try regular block device first
	file, err = os.Open(diskDevice)
	if err != nil {
		errStr := err.Error()
		// On macOS, "resource busy" means the device is mounted/in use
		if strings.Contains(errStr, "resource busy") || strings.Contains(errStr, "device busy") {
			// Try opening with explicit flags
			file, err = os.OpenFile(diskDevice, os.O_RDONLY, 0)
			if err != nil {
				// Regular device is busy - on macOS, raw devices don't work well for partition reading
				// Tell user to unmount instead
				log.Fatalf("Error: Device %s is busy (mounted).\nOn macOS, partition table reading requires unmounting first.\nTry: diskutil unmountDisk %s\nOr unmount individual partitions first.", diskDevice, diskDevice)
			}
		} else {
			log.Fatalf("Error opening disk: %v", err)
		}
	}
	defer file.Close()

	// Check if the device is a block device
	info, err := file.Stat()
	if err != nil {
		log.Fatalf("Error stating disk: %v", err)
	}

	mode := info.Mode()
	if (mode & os.ModeDevice) == 0 {
		log.Fatalf("Error: %s is not a device file.", diskDevice)
	}

	// Use the getSectorSize function
	sectorSize = uint64(getSectorSize(file))

	var diskType string
	var header gptHeader
	var partitions []gptPartition

	if !isGPTDisk(file) {
		diskType = "MBR"
		readMBRPartitions(file, diskDevice)
		return
	}
	diskType = "GPT"

	// Use Seek+Read for block devices (regular devices support Seek)
	_, err = file.Seek(512, io.SeekStart)
	if err != nil {
		log.Fatalf("Error seeking to GPT header: %v", err)
	}

	// Read header with validation
	headerBytes := make([]byte, 512)
	_, err = file.ReadAt(headerBytes, 512)
	if err != nil {
		log.Fatalf("Error reading GPT header: %v", err)
	}

	err = binary.Read(bytes.NewReader(headerBytes), binary.LittleEndian, &header)
	if err != nil {
		log.Fatalf("Error parsing GPT header: %v", err)
	}

	// Validate header CRC
	if err := validateGPTHeaderCRC(headerBytes, header.HeaderSize); err != nil {
		log.Printf("Warning: GPT header CRC validation failed: %v", err)
	}

	// Validate header size
	if header.HeaderSize < 92 || int(header.HeaderSize) > len(headerBytes) {
		log.Fatalf("Invalid GPT header size: %d", header.HeaderSize)
	}

	// Read partition entries
	tableBytes := uint64(header.NumPartEntries) * uint64(header.PartEntrySize)
	table := make([]byte, tableBytes)
	_, err = file.ReadAt(table, int64(header.PartitionEntryLBA*512))
	if err != nil {
		log.Fatalf("Error reading GPT entries: %v", err)
	}

	// Validate entries CRC
	if err := validateGPTEntriesCRC(table, header.PartEntryArrayCRC32); err != nil {
		log.Printf("Warning: GPT entries CRC validation failed: %v", err)
	}

	partitions = make([]gptPartition, 0, header.NumPartEntries)
	for i := uint32(0); i < header.NumPartEntries; i++ {
		off := uint64(i) * uint64(header.PartEntrySize)
		partition := gptPartition{}
		err := binary.Read(bytes.NewReader(table[off:off+uint64(header.PartEntrySize)]), binary.LittleEndian, &partition)
		if err != nil {
			log.Fatalf("Error reading partition entry: %v", err)
		}
		// Skip empty entries (all-zero TypeGUID)
		if !isAllZero(partition.TypeGUID[:]) {
			partitions = append(partitions, partition)
		}
	}

	// Prepare the partitions data for display
	var displayPartitions []gptPartitionDisplay
	var partID int
	for _, part := range partitions {
		if part.FirstLBA != 0 {
			partID++
			fsType := detectFileSystem(file, int64(part.FirstLBA*uint64(sectorSize)))
			totalSectors := part.LastLBA - part.FirstLBA + 1

			// Use proper GUID formatting and UTF-16LE decoding
			partName := decodeUTF16LE(part.PartitionName[:])
			typeGUID := guidToString(part.TypeGUID[:])
			uniqueGUID := guidToString(part.UniqueGUID[:])

			displayPartitions = append(displayPartitions, gptPartitionDisplay{
				Disk:          diskDevice,
				DiskType:      diskType,
				Partition:     part,
				PartitionName: fmt.Sprintf("%ss%d", diskDevice, partID),
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

	// Execute Partitions Template
	tmpl, err := template.New("partition").Parse(partitionTmpl)
	if err != nil {
		log.Fatalf("Error parsing partition template: %v", err)
	}

	for _, displayPartition := range displayPartitions {
		err = tmpl.Execute(os.Stdout, displayPartition)
		if err != nil {
			log.Fatalf("Error executing partition template: %v", err)
		}
	}
}

func readMBRPartitions(file *os.File, diskDevice string) {
	// Use Seek+Read for block devices
	_, err := file.Seek(0, io.SeekStart)
	if err != nil {
		log.Fatalf("Error seeking to MBR: %v", err)
	}

	mbr := mbrStruct{}
	err = binary.Read(file, binary.LittleEndian, &mbr)
	if err != nil {
		log.Fatalf("Error reading MBR: %v", err)
	}

	fmt.Println("Signature Found: ", mbr.Signature)

	if mbr.Signature != 0xAA55 {
		// Check if this might be a GPT disk with a protective MBR
		_, seekErr := file.Seek(512, io.SeekStart)
		if seekErr == nil {
			var gptSig [8]byte
			if readErr := binary.Read(file, binary.LittleEndian, &gptSig); readErr == nil {
				if string(gptSig[:]) == "EFI PART" {
					log.Fatalf("Invalid MBR signature (0x%04X). This appears to be a GPT disk, but GPT header detection failed.\nThe disk may be corrupted or in an unsupported format.", mbr.Signature)
				}
			}
		}

		if mbr.Signature == 0 {
			log.Fatalf("Invalid MBR signature (0x%04X). The disk may be:\n- A GPT disk (GPT detection may have failed)\n- Uninitialized or empty\n- Using an unsupported partition scheme\n\nTry using a tool like 'diskutil list %s' to verify the partition scheme.", mbr.Signature, diskDevice)
		}

		log.Fatalf("Invalid MBR signature (0x%04X). The disk does not appear to have a valid MBR partition table.\nThe disk may be uninitialized, corrupted, or using an unsupported partition scheme.", mbr.Signature)
	}

	// Check for GPT protective MBR (partition type 0xee)
	for _, part := range mbr.Partitions {
		if part.Type == 0xee && part.Sectors != 0 {
			// This is a GPT protective MBR - try to read GPT again with more tolerance
			_, seekErr := file.Seek(512, io.SeekStart)
			if seekErr == nil {
				var gptSig [8]byte
				if readErr := binary.Read(file, binary.LittleEndian, &gptSig); readErr == nil {
					if string(gptSig[:]) == "EFI PART" {
						fmt.Printf("Detected GPT protective MBR (partition type 0xee). This disk uses GPT partition scheme.\n")
						fmt.Printf("Attempting to read GPT partition table...\n\n")
						// This shouldn't happen since isGPTDisk should have caught it, but try again
						// Re-read the GPT header properly
						var gptHeader gptHeader
						file.Seek(512, io.SeekStart)
						if err := binary.Read(file, binary.LittleEndian, &gptHeader); err == nil {
							if string(gptHeader.Signature[:]) == "EFI PART" {
								log.Fatalf("This is a GPT disk. GPT detection failed initially but protective MBR (0xee) was found.\nPlease report this issue - GPT detection should have worked.")
							}
						}
						log.Fatalf("Found GPT protective MBR but could not read GPT header. The disk may be corrupted.")
					}
				}
			}
			fmt.Printf("Warning: Found partition type 0xee (GPT protective MBR).\n")
			fmt.Printf("This indicates the disk uses GPT partition scheme, but GPT detection failed.\n")
			fmt.Printf("The disk may be corrupted or the GPT header may be inaccessible.\n\n")
			break
		}
	}

	// Get device size for validation
	var sizeBytes int64
	if stat, err := file.Stat(); err == nil {
		sizeBytes = stat.Size()
	}
	if sizeBytes <= 0 {
		if size, err := getBlockDeviceSize(diskDevice); err == nil {
			sizeBytes = size
		}
	}

	fmt.Println("Partitions:")
	partNum := 1
	for _, part := range mbr.Partitions {
		if part.Sectors != 0 {
			var typeDesc string
			if part.Type == 0xee {
				typeDesc = " (GPT Protective MBR)"
			} else {
				typeDesc = ""
			}

			// Check if this is an extended partition
			if isExtendedType(part.Type) {
				fmt.Printf("  %d. Type: 0x%02x%s (Extended), FirstSector: %d, Sectors: %d, SectorSize: %d bytes, Total: %s\n",
					partNum, part.Type, typeDesc, part.FirstSector, part.Sectors, sectorSize, formatBytes(part.Sectors*uint32(sectorSize)))
				partNum++

				// Read logical partitions from extended partition
				logicalParts, err := readEBRChain(file, sizeBytes, sectorSize, part.FirstSector)
				if err != nil {
					fmt.Printf("    Warning: Could not read extended partition chain: %v\n", err)
				} else {
					for _, logicalPart := range logicalParts {
						fsType := detectFileSystem(file, int64(logicalPart.FirstSector*uint32(sectorSize)))
						fmt.Printf("    %d. Type: 0x%02x (Logical), FirstSector: %d, Sectors: %d, FileSystem: %s, SectorSize: %d bytes, Total: %s\n",
							partNum, logicalPart.Type, logicalPart.FirstSector, logicalPart.Sectors, fsType, sectorSize, formatBytes(logicalPart.Sectors*uint32(sectorSize)))
						partNum++
					}
				}
			} else {
				// Regular primary partition
				fsType := detectFileSystem(file, int64(part.FirstSector*uint32(sectorSize)))
				fmt.Printf("  %d. Type: 0x%02x%s, FirstSector: %d, Sectors: %d, FileSystem: %s, SectorSize: %d bytes, Total: %s\n",
					partNum, part.Type, typeDesc, part.FirstSector, part.Sectors, fsType, sectorSize, formatBytes(part.Sectors*uint32(sectorSize)))
				partNum++
			}
		}
	}
}

func isGPTDisk(file *os.File) bool {
	// Use Seek+Read for block devices
	_, err := file.Seek(512, io.SeekStart)
	if err != nil {
		// If seek fails, it's probably not a GPT disk or device is inaccessible
		return false
	}

	header := gptHeader{}
	err = binary.Read(file, binary.LittleEndian, &header)
	if err != nil {
		// If read fails, it's probably not a GPT disk
		return false
	}

	return string(header.Signature[:]) == "EFI PART"
}

func getSectorSize(file *os.File) int {
	var blockSize uint32
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), DKIOCGETBLOCKSIZE, uintptr(unsafe.Pointer(&blockSize)))
	if errno == 0 {
		return int(blockSize)
	}

	// Try to get physical block size as fallback
	var physBlockSize uint32
	_, _, errno = syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), DKIOCGETPHYSICALBLOCKSIZE, uintptr(unsafe.Pointer(&physBlockSize)))
	if errno == 0 {
		return int(physBlockSize)
	}

	// If ioctl fails, default to 512 bytes
	return 512
}

// detectFileSystem and detectExtFilesystem are now in filesystem_common.go

func printFirstNBytes(device string, numOfBytes int, startIndex int64) error {
	file, err := os.Open(device)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = file.Seek(startIndex, io.SeekStart)
	if err != nil {
		return err
	}

	buf := make([]byte, numOfBytes)
	_, err = io.ReadFull(file, buf)
	if err != nil {
		return err
	}

	for i := 0; i < len(buf); i += 16 {
		hexStr := ""
		charStr := ""
		for j := 0; j < 16 && i+j < len(buf); j++ {
			b := buf[i+j]
			hexStr += fmt.Sprintf("%02X ", b)
			if j == 7 {
				hexStr += " " // Extra space after 8 bytes
			}
			if isPrintable(b) {
				charStr += string(b)
			} else {
				charStr += "."
			}
		}
		fmt.Printf("%08X  %-49s  |%s|\n", startIndex+int64(i), hexStr, charStr)
	}

	return nil
}

func listDisks() {
	disks := getDiskListData()
	for _, disk := range disks {
		if disk.Mounted {
			fmt.Printf("%s %s\n", disk.Path, disk.MountInfo)
		} else {
			if disk.Size > 0 {
				fmt.Printf("%s - Total: %s %s\n", disk.Path, disk.SizeStr, disk.MountInfo)
			} else {
				fmt.Printf("%s - %s\n", disk.Path, disk.SizeStr)
			}
		}
	}
}

// getBlockDeviceSize retrieves the total size of the block device using ioctl
func getBlockDeviceSize(devPath string) (int64, error) {
	// Try regular device first
	f, err := os.Open(devPath)
	if err != nil {
		// If it's a "resource busy" error, try the raw device instead
		if strings.Contains(err.Error(), "resource busy") || strings.Contains(err.Error(), "device busy") {
			// Convert /dev/disk* to /dev/rdisk*
			rawPath := strings.Replace(devPath, "/dev/disk", "/dev/rdisk", 1)
			return getBlockDeviceSizeFromPath(rawPath)
		}
		return 0, err
	}
	defer f.Close()

	var blockSize uint32
	var blockCount uint64

	// Get block size
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), DKIOCGETBLOCKSIZE, uintptr(unsafe.Pointer(&blockSize)))
	if errno != 0 {
		blockSize = 512 // Default
	}

	// Get block count
	_, _, errno = syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), DKIOCGETBLOCKCOUNT, uintptr(unsafe.Pointer(&blockCount)))
	if errno != 0 {
		// Fallback: try to stat the device
		stat, err := f.Stat()
		if err != nil {
			return 0, err
		}
		return stat.Size(), nil
	}

	return int64(uint64(blockSize) * blockCount), nil
}

// getBlockDeviceSizeFromPath is a helper that opens the device and gets its size
func getBlockDeviceSizeFromPath(devPath string) (int64, error) {
	f, err := os.Open(devPath)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	var blockSize uint32
	var blockCount uint64

	// Get block size
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), DKIOCGETBLOCKSIZE, uintptr(unsafe.Pointer(&blockSize)))
	if errno != 0 {
		blockSize = 512 // Default
	}

	// Get block count
	_, _, errno = syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), DKIOCGETBLOCKCOUNT, uintptr(unsafe.Pointer(&blockCount)))
	if errno != 0 {
		// Fallback: try to stat the device
		stat, err := f.Stat()
		if err != nil {
			return 0, err
		}
		return stat.Size(), nil
	}

	return int64(uint64(blockSize) * blockCount), nil
}

// findMountPointForDevice tries to find where the device is mounted using mount command
func findMountPointForDevice(devPath string) (string, error) {
	cmd := exec.Command("mount")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	baseName := filepath.Base(devPath)

	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) >= 3 {
			device := parts[0]
			mountPoint := parts[2]

			// Check if this line refers to our device
			if strings.Contains(device, baseName) || device == devPath {
				return mountPoint, nil
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("no mount found for device %s", devPath)
}

// getFsSpace returns total, used, and free space for a mounted filesystem
func getFsSpace(mountPoint string) (total, used, free int64, err error) {
	var fs unix.Statfs_t
	err = unix.Statfs(mountPoint, &fs)
	if err != nil {
		return 0, 0, 0, err
	}

	total = int64(fs.Blocks) * int64(fs.Bsize)
	free = int64(fs.Bfree) * int64(fs.Bsize)
	available := int64(fs.Bavail) * int64(fs.Bsize)
	used = total - available
	return total, used, free, nil
}

func hasReadPermission(device string) bool {
	// On macOS, for /dev/disk* devices, try raw device first as it's more reliable
	if strings.HasPrefix(device, "/dev/disk") && !strings.HasPrefix(device, "/dev/rdisk") {
		rawPath := strings.Replace(device, "/dev/disk", "/dev/rdisk", 1)
		file, err := os.OpenFile(rawPath, os.O_RDONLY, 0)
		if err == nil {
			file.Close()
			return true
		}
		// If raw device fails, fall through to try regular device
	}

	// Try regular device
	file, err := os.OpenFile(device, os.O_RDONLY, 0)
	if err != nil {
		return false
	}
	file.Close()
	return true
}

func readdisk(device, outputfile, compressionAlgorithm string) {
	// Open the disk device file
	disk, err := os.Open(device)
	if err != nil {
		fmt.Println("Failed to open Device:", device)
		return
	}
	defer disk.Close()

	// Attempt to get total size for estimation
	var totalSize int64

	// If device is a raw device, try converting to regular device first for size detection
	devPathForSize := device
	if strings.HasPrefix(device, "/dev/rdisk") {
		devPathForSize = strings.Replace(device, "/dev/rdisk", "/dev/disk", 1)
	}

	// Try multiple methods to get device size
	if stat, err := os.Stat(devPathForSize); err == nil {
		totalSize = stat.Size()
	}

	// If stat failed or returned 0, try ioctl method
	if totalSize <= 0 {
		if size, err := getBlockDeviceSize(devPathForSize); err == nil && size > 0 {
			totalSize = size
		}
	}

	// If still 0, try the original device path (for raw devices)
	if totalSize <= 0 && devPathForSize != device {
		if stat, err := os.Stat(device); err == nil && stat.Size() > 0 {
			totalSize = stat.Size()
		}
		if totalSize <= 0 {
			if size, err := getBlockDeviceSize(device); err == nil && size > 0 {
				totalSize = size
			}
		}
	}

	// Debug output if we couldn't get size
	if totalSize <= 0 {
		fmt.Printf("Warning: Could not determine device size. Estimated time will be unavailable.\n")
	}

	// Use the common compression function
	err = compressFromReader(disk, outputfile, compressionAlgorithm, totalSize)
	if err != nil {
		fmt.Printf("Error during compression: %v\n", err)
		return
	}
}

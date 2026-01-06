package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"text/template"
	"unsafe"

	"golang.org/x/sys/unix"
)

func printDiskBytes(diskDevice string, numOfBytes int, startIndex int64) {
	err := printFirstNBytes(diskDevice, numOfBytes, startIndex)
	if err != nil {
		fmt.Printf("Error reading %d bytes from index %d, error: %v\n", numOfBytes, startIndex, err)
	}
}

func listPartitions(diskDevice string) {
	var diskType string
	//Start the partition table parsing
	file, err := os.Open(diskDevice)
	if err != nil {
		log.Fatalf("Error opening disk: %v", err)
	}
	defer file.Close()

	// Check if the device is a block device, as seeking on a non-block device (like /dev/nvme0) will fail.
	info, err := file.Stat()
	if err != nil {
		log.Fatalf("Error stating disk: %v", err)
	}

	// On Linux, block devices will appear as devices but not character devices.
	// Check if it's a character device (e.g., an NVMe controller) or if it's not a device at all.
	mode := info.Mode()
	if (mode & os.ModeDevice) == 0 {
		log.Fatalf("Error: %s is not a device file.", diskDevice)
	}
	if (mode & os.ModeCharDevice) != 0 {
		log.Fatalf("Error: %s is a character device (e.g., NVMe controller), not a block device. Use the block device namespace instead, e.g. /dev/nvme0n1.", diskDevice)
	}

	// Use the getSectorSize function after verifying the device is block-seekable.
	sectorSize = uint64(getSectorSize(file))

	if !isGPTDisk(file) {
		diskType = "MBR"
		_, err := file.Seek(0, 0)
		if err != nil {
			log.Fatalf("Error seeking disk: %v", err)
		}
		readMBRPartitions(file, diskDevice)
		return
	}
	diskType = "GPT"

	// Read header with validation
	headerBytes := make([]byte, 512)
	_, err = file.ReadAt(headerBytes, 512)
	if err != nil {
		log.Fatalf("Error reading GPT header: %v", err)
	}

	header := gptHeader{}
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

	partitions := make([]gptPartition, 0, header.NumPartEntries)
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

	tmpl, err := template.New("disk").Parse(partitionTmpl)
	if err != nil {
		log.Fatalf("Error parsing disk template: %v", err)
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

	// Execute Partitions Template
	tmpl, err = template.New("partition").Parse(partitionTmpl)
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
	mbr := mbrStruct{}
	err := binary.Read(file, binary.LittleEndian, &mbr)
	if err != nil {
		log.Fatalf("Error reading MBR: %v", err)
	}

	fmt.Println("Signature Found: ", mbr.Signature)

	if mbr.Signature != 0xAA55 {
		log.Fatalf("Invalid MBR signature")
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
	for i, part := range mbr.Partitions {
		if part.Sectors != 0 {
			// Check if this is an extended partition
			if isExtendedType(part.Type) {
				fmt.Printf("  %d. Type: 0x%02x (Extended), FirstSector: %d, Sectors: %d, SectorSize: %d bytes, Total: %s\n",
					partNum, part.Type, part.FirstSector, part.Sectors, sectorSize, formatBytes(part.Sectors*uint32(sectorSize)))
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
				fmt.Printf("  %d. Type: 0x%02x, FirstSector: %d, Sectors: %d, FileSystem: %s, SectorSize: %d bytes, Total: %s\n",
					partNum, part.Type, part.FirstSector, part.Sectors, fsType, sectorSize, formatBytes(part.Sectors*uint32(sectorSize)))
				partNum++
			}
		}
	}
}

func isGPTDisk(file *os.File) bool {
	_, err := file.Seek(512, 0)
	if err != nil {
		log.Fatalf("Error seeking disk: %v", err)
	}

	header := gptHeader{}
	err = binary.Read(file, binary.LittleEndian, &header)
	if err != nil {
		log.Fatalf("Error reading GPT header: %v", err)
	}

	return string(header.Signature[:]) == "EFI PART"
}

func getSectorSize(file *os.File) int {
	sectorSize, err := unix.IoctlGetInt(int(file.Fd()), unix.BLKSSZGET)
	if err == nil {
		return sectorSize
	}

	// If ioctl fails, fallback to reading from sysfs
	devName := filepath.Base(file.Name()) // e.g. /dev/nvme0 -> nvme0
	hwSectorSizePath := "/sys/class/block/" + devName + "/queue/hw_sector_size"
	data, err := os.ReadFile(hwSectorSizePath)
	if err == nil {
		szStr := strings.TrimSpace(string(data))
		sz, convErr := strconv.Atoi(szStr)
		if convErr == nil && sz > 0 {
			return sz
		}
	}

	// If we cannot get it from sysfs, default to 512 bytes
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

func checkWSL() bool {
	data, err := os.ReadFile("/proc/version")
	if err != nil {
		return false
	}

	WSL := strings.Contains(strings.ToLower(string(data)), "wsl")

	if WSL {
		fmt.Println(red+blink, "Running inside WSL!", reset)
	}

	return WSL
}

func listDisks() {
	blockDevices, err := os.ReadDir("/sys/class/block")
	if err != nil {
		fmt.Printf("Error reading /sys/class/block: %v\n", err)
		return
	}

	for _, bd := range blockDevices {
		devName := bd.Name()

		// Filter out devices that are known not to be physical disks
		// Define the prefixes to exclude
		excludePrefixes := []string{"loop", "zram", "ram"}

		// Check if devName starts with any of the excluded prefixes
		shouldContinue := false
		for _, prefix := range excludePrefixes {
			if strings.HasPrefix(devName, prefix) {
				shouldContinue = true
				break
			}
		}

		if shouldContinue {
			continue
		}

		devPath := "/dev/" + devName

		// Get the total size of the block device
		totalSize, err := getBlockDeviceSize(devPath)
		if err != nil {
			fmt.Printf("Error getting size for %s: %v\n", devPath, err)
			continue
		}

		// Attempt to find a mount point for this device
		mountPoint, err := findMountPointForDevice(devPath)
		if err != nil {
			// No mount point found
			fmt.Printf("%s - Total: %s (No filesystem mount found)\n", devPath, formatBytes(totalSize))
			continue
		}

		// Get filesystem usage if mounted
		totalFs, usedFs, freeFs, err := getFsSpace(mountPoint)
		if err != nil {
			fmt.Printf("%s - Total: %d bytes, error reading filesystem: %v\n", devPath, totalSize, err)
			continue
		}

		fmt.Printf("%s (mounted on %s) - Total: %s, Used: %s, Free: %s\n",
			devPath, mountPoint, formatBytes(totalFs), formatBytes(usedFs), formatBytes(freeFs))
	}
}

// getBlockDeviceSize retrieves the total size of the block device using an ioctl call
func getBlockDeviceSize(devPath string) (int64, error) {
	f, err := os.Open(devPath)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	var size int64
	_, _, e := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), BLKGETSIZE64, uintptr(unsafe.Pointer(&size)))
	if e != 0 {
		return 0, fmt.Errorf("ioctl BLKGETSIZE64 failed: %v", e)
	}
	return size, nil
}

// findMountPointForDevice tries to find where the device is mounted by reading /proc/self/mountinfo
func findMountPointForDevice(devPath string) (string, error) {
	f, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return "", err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Split(line, " - ")
		if len(parts) < 2 {
			continue
		}
		beforeDash := parts[0]
		afterDash := parts[1]

		beforeFields := strings.Split(beforeDash, " ")
		if len(beforeFields) < 5 {
			continue
		}

		mountPoint := beforeFields[4]
		afterFields := strings.Split(afterDash, " ")
		if len(afterFields) < 3 {
			continue
		}
		mountedDev := afterFields[1]

		if mountedDev == devPath {
			return mountPoint, nil
		}
	}

	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("no mount found for device %s", devPath)
}

// getFsSpace returns total, used, and free space for a mounted filesystem
func getFsSpace(mountPoint string) (total, used, free int64, err error) {
	var fs syscall.Statfs_t
	err = syscall.Statfs(mountPoint, &fs)
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
	checkWSL()
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
	if stat, err := os.Stat(device); err == nil {
		totalSize = stat.Size()
	}

	// Use the common compression function
	err = compressFromReader(disk, outputfile, compressionAlgorithm, totalSize)
	if err != nil {
		fmt.Printf("Error during compression: %v\n", err)
		return
	}
}

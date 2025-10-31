//go:build darwin

package main

import (
	"archive/zip"
	"bufio"
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
	"time"
	"unsafe"

	"github.com/dsnet/compress/bzip2"
	"github.com/gosuri/uilive"
	"github.com/klauspost/compress/gzip"
	"github.com/klauspost/compress/s2"
	"github.com/klauspost/compress/snappy"
	"github.com/klauspost/compress/zlib"
	"github.com/klauspost/compress/zstd"
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

	err = binary.Read(file, binary.LittleEndian, &header)
	if err != nil {
		log.Fatalf("Error reading GPT header: %v", err)
	}

	_, err = file.Seek(int64(header.PartitionEntryLBA*512), io.SeekStart)
	if err != nil {
		log.Fatalf("Error seeking to partition entries: %v", err)
	}

	partitions = make([]gptPartition, 0, header.NumPartEntries)
	for i := uint32(0); i < header.NumPartEntries; i++ {
		partition := gptPartition{}
		err := binary.Read(file, binary.LittleEndian, &partition)
		if err != nil {
			log.Fatalf("Error reading partition entry: %v", err)
		}
		if partition.FirstLBA != 0 {
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

			displayPartitions = append(displayPartitions, gptPartitionDisplay{
				Disk:          diskDevice,
				DiskType:      diskType,
				Partition:     part,
				PartitionName: fmt.Sprintf("%ss%d", diskDevice, partID),
				Name:          string(part.PartitionName[:]),
				Filesystem:    fsType,
				TotalSectors:  totalSectors,
				SectorSize:    sectorSize,
				Total:         formatBytes(totalSectors * sectorSize),
				TypeGUIDStr:   fmt.Sprintf("%x", part.TypeGUID),
				UniqueGUIDStr: fmt.Sprintf("%x", part.UniqueGUID),
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

	fmt.Println("Partitions:")
	for i, part := range mbr.Partitions {
		if part.Sectors != 0 {
			var typeDesc string
			if part.Type == 0xee {
				typeDesc = " (GPT Protective MBR)"
			} else {
				typeDesc = ""
			}
			fsType := detectFileSystem(file, int64(part.FirstSector*uint32(sectorSize)))
			fmt.Printf("  %d. Type: 0x%02x%s, FirstSector: %d, Sectors: %d, FileSystem: %s, SectorSize: %d bytes, Total: %s\n",
				i+1, part.Type, typeDesc, part.FirstSector, part.Sectors, fsType, sectorSize, formatBytes(part.Sectors*uint32(sectorSize)))
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
	listDisksFromDev()
}

func listDisksFromDev() {
	// List disks from /dev/disk*
	entries, err := os.ReadDir("/dev")
	if err != nil {
		fmt.Printf("Error reading /dev: %v\n", err)
		return
	}

	diskMap := make(map[string]bool)
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, "disk") && len(name) > 4 {
			// Check if it matches pattern "disk" + digits (e.g., "disk0", "disk1")
			// or "disk" + digits + "s" + digits (e.g., "disk0s1")
			if len(name) == 5 {
				// Check if it's exactly "disk" + single digit (e.g., "disk0")
				if name[4] >= '0' && name[4] <= '9' {
					diskMap[name] = true
				}
			} else if len(name) > 5 && name[4] >= '0' && name[4] <= '9' {
				// Check if it's "disk" + digits + "s" (a partition)
				if idx := strings.Index(name[4:], "s"); idx > 0 {
					// Extract base disk name (e.g., "disk0" from "disk0s1")
					baseName := name[:4+idx]
					diskMap[baseName] = true
				} else {
					// It's "disk" + multiple digits (e.g., "disk10")
					// Check if all characters after "disk" are digits
					allDigits := true
					for i := 4; i < len(name); i++ {
						if name[i] < '0' || name[i] > '9' {
							allDigits = false
							break
						}
					}
					if allDigits {
						diskMap[name] = true
					}
				}
			}
		}
	}

	for diskName := range diskMap {
		devPath := "/dev/" + diskName
		totalSize, err := getBlockDeviceSize(devPath)
		if err != nil {
			// Handle different error types
			errStr := err.Error()
			if strings.Contains(errStr, "permission") || strings.Contains(errStr, "not permitted") {
				fmt.Printf("%s - (Size unavailable: requires root access)\n", devPath)
				continue
			} else if strings.Contains(errStr, "resource busy") || strings.Contains(errStr, "device busy") {
				// Try raw device as fallback
				rawPath := strings.Replace(devPath, "/dev/disk", "/dev/rdisk", 1)
				totalSize, err = getBlockDeviceSizeFromPath(rawPath)
				if err != nil {
					fmt.Printf("%s - (Device busy, could not read size)\n", devPath)
					continue
				}
				// Continue with the size we got from raw device
			} else {
				fmt.Printf("%s - Error getting size: %v\n", devPath, err)
				continue
			}
		}

		// Try to find mount point
		mountPoint, err := findMountPointForDevice(devPath)
		if err != nil {
			fmt.Printf("%s - Total: %s (No filesystem mount found)\n", devPath, formatBytes(totalSize))
			continue
		}

		// Get filesystem usage if mounted
		totalFs, usedFs, freeFs, err := getFsSpace(mountPoint)
		if err != nil {
			fmt.Printf("%s - Total: %s, error reading filesystem: %v\n", devPath, formatBytes(totalSize), err)
			continue
		}

		fmt.Printf("%s (mounted on %s) - Total: %s, Used: %s, Free: %s\n",
			devPath, mountPoint, formatBytes(totalFs), formatBytes(usedFs), formatBytes(freeFs))
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

type countingWriter struct {
	w     io.Writer
	count int64
}

func (cw *countingWriter) Write(p []byte) (int, error) {
	n, err := cw.w.Write(p)
	cw.count += int64(n)
	return n, err
}

func readdisk(device, outputfile, compressionAlgorithm string) {
	// Open the disk device file
	disk, err := os.Open(device)
	if err != nil {
		fmt.Println("Failed to open Device:", device)
		return
	}
	defer disk.Close()

	// Determine file extension based on compression algorithm
	var extension string
	switch compressionAlgorithm {
	case "gzip":
		extension = ".gz"
	case "zlib":
		extension = ".zlib"
	case "bzip2":
		extension = ".bz2"
	case "snappy":
		extension = ".snappy"
	case "s2":
		extension = ".s2"
	case "zstd":
		extension = ".zst"
	case "zip":
		extension = ".zip"
	default:
		fmt.Println("Unsupported compression algorithm:", compressionAlgorithm)
		return
	}

	outputfile = outputfile + extension

	// Create a new file to write the data to
	output, err := os.Create(outputfile)
	if err != nil {
		fmt.Println("Failed to create output file:", outputfile)
		return
	}
	defer output.Close()

	// Wrap output with a countingWriter
	cw := &countingWriter{w: output}

	var compressedWriter io.Writer
	var zipWriter *zip.Writer

	// Create the compression writer based on the chosen algorithm
	switch compressionAlgorithm {
	case "gzip":
		compressedWriter = gzip.NewWriter(cw)
	case "zlib":
		compressedWriter = zlib.NewWriter(cw)
	case "bzip2":
		compressedWriter, err = bzip2.NewWriter(cw, &bzip2.WriterConfig{})
		if err != nil {
			fmt.Println("Failed to create bzip2 writer:", err)
			return
		}
	case "snappy":
		compressedWriter = snappy.NewBufferedWriter(cw)
	case "s2":
		compressedWriter = s2.NewWriter(cw)
	case "zstd":
		compressedWriter, err = zstd.NewWriter(cw)
		if err != nil {
			fmt.Println("Failed to create zstd writer:", err)
			return
		}
	case "zip":
		zipWriter = zip.NewWriter(cw)
		zipFile, err := zipWriter.Create("compressedData")
		if err != nil {
			fmt.Println("Failed to create zip entry:", err.Error())
			return
		}
		compressedWriter = zipFile
	}

	if err != nil {
		fmt.Println("Failed to create compression writer:", err.Error())
		return
	}

	fmt.Printf("Writing to Image: %s\n", outputfile)

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

	start := time.Now()

	// Setup uilive for dynamic output
	writer := uilive.New()
	writer.Start() // start the live writer

	var (
		bytesRead  int64
		count      int
		byteCount  = 16384
		buf        = make([]byte, byteCount)
		lastUpdate = time.Now()
	)

	for {
		n, err := disk.Read(buf)
		if n > 0 {
			_, wErr := compressedWriter.Write(buf[:n])
			if wErr != nil {
				fmt.Fprintln(writer.Bypass(), "Failed to write compressed stream:", wErr.Error())
				writer.Stop()
				return
			}

			bytesRead += int64(n)
			count++

			// Update once every second
			if time.Since(lastUpdate) >= time.Second {
				elapsed := time.Since(start).Truncate(time.Second)
				var estimateStr string
				if totalSize > 0 && bytesRead > 0 {
					elapsedSeconds := time.Since(start).Seconds()
					if elapsedSeconds > 0 {
						rate := float64(bytesRead) / elapsedSeconds
						remaining := float64(totalSize-bytesRead) / rate
						if remaining < 0 {
							remaining = 0
						}
						// Format as human-readable time (e.g., "1m30s" or "45s")
						if remaining < 60 {
							estimateStr = fmt.Sprintf("%.0fs", remaining)
						} else if remaining < 3600 {
							estimateStr = fmt.Sprintf("%.0fm%.0fs", remaining/60, float64(int(remaining)%60))
						} else {
							hours := int(remaining / 3600)
							mins := int((remaining - float64(hours)*3600) / 60)
							secs := int(remaining - float64(hours)*3600 - float64(mins)*60)
							estimateStr = fmt.Sprintf("%dh%dm%ds", hours, mins, secs)
						}
					} else {
						estimateStr = "N/A"
					}
				} else {
					estimateStr = "N/A"
				}

				elapsedSeconds := time.Since(start).Seconds()
				readBps := float64(bytesRead) / elapsedSeconds
				writeBps := float64(cw.count) / elapsedSeconds

				fmt.Fprintf(writer,
					"Byte Count: Read: %s (%d bytes), Written: %s (%d bytes)\n",
					formatBytes(bytesRead), bytesRead,
					formatBytes(cw.count), cw.count)
				fmt.Fprintf(writer, "Elapsed Time: %s\n", elapsed)
				fmt.Fprintf(writer, "Estimated Time: %s\n", estimateStr)
				fmt.Fprintf(writer, "Read Speed: %s\n", formatSpeed(readBps))
				fmt.Fprintf(writer, "Write Speed: %s\n", formatSpeed(writeBps))

				writer.Flush()
				lastUpdate = time.Now()
			}
		}

		if err != nil {
			if err == io.EOF {
				// Final update at the end
				elapsed := time.Since(start).Truncate(time.Second)
				var estimateStr string
				if totalSize > 0 && bytesRead > 0 {
					elapsedSeconds := time.Since(start).Seconds()
					if elapsedSeconds > 0 {
						rate := float64(bytesRead) / elapsedSeconds
						remaining := float64(totalSize-bytesRead) / rate
						if remaining < 0 {
							remaining = 0
						}
						// Format as human-readable time (e.g., "1m30s" or "45s")
						if remaining < 60 {
							estimateStr = fmt.Sprintf("%.0fs", remaining)
						} else if remaining < 3600 {
							estimateStr = fmt.Sprintf("%.0fm%.0fs", remaining/60, float64(int(remaining)%60))
						} else {
							hours := int(remaining / 3600)
							mins := int((remaining - float64(hours)*3600) / 60)
							secs := int(remaining - float64(hours)*3600 - float64(mins)*60)
							estimateStr = fmt.Sprintf("%dh%dm%ds", hours, mins, secs)
						}
					} else {
						estimateStr = "N/A"
					}
				} else {
					estimateStr = "N/A"
				}

				elapsedSeconds := time.Since(start).Seconds()
				readBps := float64(bytesRead) / elapsedSeconds
				writeBps := float64(cw.count) / elapsedSeconds

				fmt.Fprintf(writer,
					"Byte Count: Read: %s (%d bytes), Written: %s (%d bytes)\n",
					formatBytes(bytesRead), bytesRead,
					formatBytes(cw.count), cw.count)
				fmt.Fprintf(writer, "Elapsed Time: %s\n", elapsed)
				fmt.Fprintf(writer, "Estimated Time: %s\n", estimateStr)
				fmt.Fprintf(writer, "Read Speed: %s\n", formatSpeed(readBps))
				fmt.Fprintf(writer, "Write Speed: %s\n", formatSpeed(writeBps))
				writer.Flush()
				break
			} else {
				fmt.Fprintln(writer.Bypass(), "Error reading from disk:", err.Error())
				writer.Stop()
				return
			}
		}
	}

	writer.Stop() // stop the live writer

	totalBytes := bytesRead
	fmt.Println() // new line after finishing updates
	fmt.Println("Written:", formatBytes(totalBytes), "(", totalBytes, "bytes )")

	// Close zipWriter if we have one
	if zipWriter != nil {
		err := zipWriter.Close()
		if err != nil {
			fmt.Println("Failed to close zip writer:", err.Error())
		}
	} else {
		// If the compression writer implements Close, call it
		if wc, ok := compressedWriter.(io.WriteCloser); ok {
			wc.Close()
		}
	}

	finalElapsed := time.Since(start).Truncate(time.Second)
	finalElapsedSeconds := time.Since(start).Seconds()
	finalReadBps := float64(bytesRead) / finalElapsedSeconds
	finalWriteBps := float64(cw.count) / finalElapsedSeconds

	// Calculate compression ratio: original_size / compressed_size
	var compressionRatio string
	if cw.count > 0 {
		ratio := float64(totalBytes) / float64(cw.count)
		compressionRatio = fmt.Sprintf("%.2f:1", ratio)
	} else {
		compressionRatio = "N/A"
	}

	fmt.Printf("Total actual time: %s (%s read, %s write) Compression ratio: %s\n",
		finalElapsed, formatSpeed(finalReadBps), formatSpeed(finalWriteBps), compressionRatio)
}

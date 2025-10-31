package main

import (
	"archive/zip"
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
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
		readMBRPartitions(file)
		return
	}
	diskType = "GPT"

	_, err = file.Seek(512, 0)
	if err != nil {
		log.Fatalf("Error seeking disk: %v", err)
	}

	header := gptHeader{}
	err = binary.Read(file, binary.LittleEndian, &header)
	if err != nil {
		log.Fatalf("Error reading GPT header: %v", err)
	}

	_, err = file.Seek(int64(header.PartitionEntryLBA*512), 0)
	if err != nil {
		log.Fatalf("Error seeking disk: %v", err)
	}

	partitions := make([]gptPartition, header.NumPartEntries)

	for i := uint32(0); i < header.NumPartEntries; i++ {
		partition := gptPartition{}
		_, err = file.Seek(int64(header.PartitionEntryLBA*512)+int64(i*header.PartEntrySize), 0)
		if err != nil {
			log.Fatalf("Error seeking disk: %v", err)
		}

		err := binary.Read(file, binary.LittleEndian, &partition)
		if err != nil {
			log.Fatalf("Error reading partition entry: %v", err)
		}
		if partition.FirstLBA != 0 {
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

			displayPartitions = append(displayPartitions, gptPartitionDisplay{
				Disk:          diskDevice,
				DiskType:      diskType,
				Partition:     part,
				PartitionName: fmt.Sprintf("%s%d", diskDevice, partID),
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

func readMBRPartitions(file *os.File) {
	mbr := mbrStruct{}
	err := binary.Read(file, binary.LittleEndian, &mbr)
	if err != nil {
		log.Fatalf("Error reading MBR: %v", err)
	}

	fmt.Println("Signature Found: ", mbr.Signature)

	if mbr.Signature != 0xAA55 {
		log.Fatalf("Invalid MBR signature")
	}

	fmt.Println("Partitions:")
	for i, part := range mbr.Partitions {
		if part.Sectors != 0 {
			fsType := detectFileSystem(file, int64(part.FirstSector*uint32(sectorSize)))
			fmt.Printf("  %d. Type: 0x%02x, FirstSector: %d, Sectors: %d, FileSystem: %s, SectorSize: %d bytes, Total: %s\n", i+1, part.Type, part.FirstSector, part.Sectors, fsType, sectorSize, formatBytes(part.Sectors*uint32(sectorSize)))
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
	if stat, err := os.Stat(device); err == nil {
		totalSize = stat.Size()
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
					rate := float64(bytesRead) / time.Since(start).Seconds()
					remaining := float64(totalSize-bytesRead) / rate
					if remaining < 0 {
						remaining = 0
					}
					estimateStr = fmt.Sprintf("%.0fs", remaining)
				} else {
					estimateStr = "N/A"
				}

				readMBps := (float64(bytesRead) / (1024.0 * 1024.0)) / time.Since(start).Seconds()
				writeMBps := (float64(cw.count) / (1024.0 * 1024.0)) / time.Since(start).Seconds()

				fmt.Fprintf(writer,
					"Byte Count: Read: %s (%d bytes), Written: %s (%d bytes)\n",
					formatBytes(bytesRead), bytesRead,
					formatBytes(cw.count), cw.count)
				fmt.Fprintf(writer, "Elapsed Time: %s\n", elapsed)
				fmt.Fprintf(writer, "Estimated Time: %s\n", estimateStr)
				fmt.Fprintf(writer, "Read Speed: %.2f MB/s\n", readMBps)
				fmt.Fprintf(writer, "Write Speed: %.2f MB/s\n", writeMBps)

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
					rate := float64(bytesRead) / time.Since(start).Seconds()
					remaining := float64(totalSize-bytesRead) / rate
					if remaining < 0 {
						remaining = 0
					}
					estimateStr = fmt.Sprintf("%.0fs", remaining)
				} else {
					estimateStr = "N/A"
				}

				readMBps := (float64(bytesRead) / (1024.0 * 1024.0)) / time.Since(start).Seconds()
				writeMBps := (float64(cw.count) / (1024.0 * 1024.0)) / time.Since(start).Seconds()

				fmt.Fprintf(writer,
					"Byte Count: Read: %s (%d bytes), Written: %s (%d bytes)\n",
					formatBytes(bytesRead), bytesRead,
					formatBytes(cw.count), cw.count)
				fmt.Fprintf(writer, "Elapsed Time: %s\n", elapsed)
				fmt.Fprintf(writer, "Estimated Time: %s\n", estimateStr)
				fmt.Fprintf(writer, "Read Speed: %.2f MB/s\n", readMBps)
				fmt.Fprintf(writer, "Write Speed: %.2f MB/s\n", writeMBps)
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
	finalReadMBps := (float64(bytesRead) / (1024.0 * 1024.0)) / time.Since(start).Seconds()
	finalWriteMBps := (float64(cw.count) / (1024.0 * 1024.0)) / time.Since(start).Seconds()

	// Calculate compression ratio: original_size / compressed_size
	var compressionRatio string
	if cw.count > 0 {
		ratio := float64(totalBytes) / float64(cw.count)
		compressionRatio = fmt.Sprintf("%.2f:1", ratio)
	} else {
		compressionRatio = "N/A"
	}

	fmt.Printf("Total actual time: %s (%.2f MB/s read, %.2f MB/s write) Compression ratio: %s\n",
		finalElapsed, finalReadMBps, finalWriteMBps, compressionRatio)
}

//go:build linux
// +build linux

package main

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"unicode"

	"github.com/dsnet/compress/bzip2"
	"github.com/klauspost/compress/gzip"
	"github.com/klauspost/compress/snappy"
	"github.com/klauspost/compress/zlib"
	"github.com/klauspost/compress/zstd"

	"golang.org/x/sys/unix"
)

func listPartitions(diskDevice string) {
	checkWSL()
	listPartitionsLinux(diskDevice)
}

func listDisks() {
	listRealDisksLinux()
}

func printDiskBytes(diskDevice string, numOfBytes int) {
	checkWSL()
	err := printFirstNBytes(diskDevice, numOfBytes)
	if err != nil {
		fmt.Println("Error reading the first N bytes:", err)
	}
}

func listPartitions(diskDevice string) {
	//Start the partition table parsing
	file, err := os.Open(diskDevice)
	if err != nil {
		log.Fatalf("Error opening disk: %v", err)
	}
	defer file.Close()

	sectorSize = uint64(getSectorSize(file))

	if !isGPTDisk(file) {
		fmt.Println("MBR disk")
		_, err := file.Seek(0, 0)
		if err != nil {
			log.Fatalf("Error seeking disk: %v", err)
		}
		readMBRPartitions(file)
		return
	}
	fmt.Println("GPT disk")

	_, err = file.Seek(512, 0)
	if err != nil {
		log.Fatalf("Error seeking disk: %v", err)
	}

	header := GPTHeader{}
	err = binary.Read(file, binary.LittleEndian, &header)
	if err != nil {
		log.Fatalf("Error reading GPT header: %v", err)
	}

	_, err = file.Seek(int64(header.PartitionEntryLBA*512), 0)
	if err != nil {
		log.Fatalf("Error seeking disk: %v", err)
	}

	partitions := make([]GPTPartition, header.NumPartEntries)

	for i := uint32(0); i < header.NumPartEntries; i++ {
		partition := GPTPartition{}
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

	fmt.Printf("Disk: %s\n", diskDevice)
	fmt.Println("Partitions:")
	for i, part := range partitions {
		if part.FirstLBA != 0 {
			partitionName := string(part.PartitionName[:])
			totalSectors := part.LastLBA - part.FirstLBA + 1

			fsType := DetectFileSystem(file, int64(part.FirstLBA*uint64(sectorSize)))
			fmt.Printf("  %d. TypeGUID: %x, UniqueGUID: %x, FirstLBA: %d, LastLBA: %d, Name: %s, FileSystem: %s, SectorSize: %d bytes, TotalSectors: %d, Total: %d bytes\n", i+1, part.TypeGUID, part.UniqueGUID, part.FirstLBA, part.LastLBA, partitionName, fsType, sectorSize, totalSectors, totalSectors*sectorSize/1024/1024)
		}
	}
}

func readMBRPartitions(file *os.File) {
	mbr := MBR{}
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
			fsType := DetectFileSystem(file, int64(part.FirstSector*uint32(sectorSize)))
			fmt.Printf("  %d. Type: 0x%02x, FirstSector: %d, Sectors: %d, FileSystem: %s, SectorSize: %d bytes, Total: %d bytes\n", i+1, part.Type, part.FirstSector, part.Sectors, fsType, sectorSize, part.Sectors*uint32(sectorSize)/1024/1024)
		}
	}
}

func isGPTDisk(file *os.File) bool {
	_, err := file.Seek(512, 0)
	if err != nil {
		log.Fatalf("Error seeking disk: %v", err)
	}

	header := GPTHeader{}
	err = binary.Read(file, binary.LittleEndian, &header)
	if err != nil {
		log.Fatalf("Error reading GPT header: %v", err)
	}

	return string(header.Signature[:]) == "EFI PART"
}

func getSectorSize(file *os.File) int {
	var sectorSize int
	sectorSize, err := unix.IoctlGetInt(int(file.Fd()), unix.BLKSSZGET)
	if err != nil {
		log.Fatalf("Error getting sector size: %v", err)
	}
	return sectorSize
}

func DetectFileSystem(file *os.File, offset int64) string {
	fsList := []FileSystem{
		{Name: "Amiga FFS", Signature: []byte{0x44, 0x4F, 0x53}, Offset: 0x3400},
		{Name: "APFS", Signature: []byte("NXSB"), Offset: 0},
		{Name: "AUFS (SunOS)", Signature: []byte{0x2a, 0x2a, 0x2a, 0x14}, Offset: 0},
		{Name: "Btrfs", Signature: []byte("_BHRfS_M"), Offset: 0x40},
		{Name: "BeFS (BeOS)", Signature: []byte{0x69, 0x19, 0x01, 0x00}, Offset: 0x414},
		{Name: "CramFS", Signature: []byte{0x28, 0xcd, 0x3d, 0x45}, Offset: 0},
		{Name: "CramFS (swapped)", Signature: []byte{0x45, 0x3d, 0xcd, 0x28}, Offset: 0},
		{Name: "EFS (Ext2 Encrypted)", Signature: []byte{0x53, 0xef, 0x01, 0x00}, Offset: 0x438},
		{Name: "exFAT", Signature: []byte{0x45, 0x58, 0x46, 0x41, 0x54}, Offset: 3},
		{Name: "FAT32", Signature: []byte{0x55, 0xaa}, Offset: 0x1fe},
		{Name: "FAT12/16", Signature: []byte{0x55, 0xaa}, Offset: 0x1fe},
		{Name: "F2FS", Signature: []byte{0xF2, 0xF5, 0x20, 0x10}, Offset: 0x400},
		{Name: "HAMMER (DragonFly BSD)", Signature: []byte{0x34, 0xC1, 0x03, 0x49}, Offset: 0x200},
		{Name: "HAMMER2 (DragonFly BSD)", Signature: []byte("H2"), Offset: 0x08},
		{Name: "HPFS", Signature: []byte{0xf8, 0x2a, 0x2b, 0x01}, Offset: 0},
		{Name: "HFS", Signature: []byte{'B', 'D', 0x00, 0x01}, Offset: 0x400},
		{Name: "HFS+", Signature: []byte{'H', '+', 0x00, 0x04}, Offset: 0x400},
		{Name: "ISO9660", Signature: []byte("CD001"), Offset: 0x8001},
		{Name: "JFS", Signature: []byte("JFS1"), Offset: 0x8004},
		{Name: "Swap (Linux)", Signature: []byte("SWAPSPACE2"), Offset: 0x40C0},
		{Name: "LVM", Signature: []byte("LVM2 001"), Offset: 0x218},
		{Name: "LVM", Signature: []byte("LABELONE"), Offset: 0x204},
		{Name: "Minix (30 char)", Signature: []byte{0x18, 0x03, 0x78, 0x56}, Offset: 0x410},
		{Name: "Minix (62 char)", Signature: []byte{0x18, 0x04, 0x78, 0x56}, Offset: 0x410},
		{Name: "Minix v2 (30 char)", Signature: []byte{0x24, 0x05, 0x19, 0x05}, Offset: 0x410},
		{Name: "Minix v2 (62 char)", Signature: []byte{0x24, 0x05, 0x19, 0x08}, Offset: 0x410},
		{Name: "NILFS2", Signature: []byte{0x34, 0x34, 0x5E, 0x1C}, Offset: 0x400},
		{Name: "NTFS", Signature: []byte("NTFS"), Offset: 3},
		{Name: "OCFS2", Signature: []byte("OCFSV2"), Offset: 0x2000},
		{Name: "QNX6", Signature: []byte("QNX6"), Offset: 0x4},
		{Name: "ReiserFS", Signature: []byte{0x34, 0x34}, Offset: 0x10034},
		{Name: "Reiser4", Signature: []byte{0x4A, 0x4A}, Offset: 0x10034},
		{Name: "RomFS", Signature: []byte("-rom1fs-"), Offset: 0},
		{Name: "SkyFS (Haiku)", Signature: []byte{0x79, 0x30, 0x33, 0x01}, Offset: 0x414},
		{Name: "SysV", Signature: []byte{0xfd, 0x37, 0x59, 0x5F}, Offset: 0},
		{Name: "SquashFS", Signature: []byte{0x73, 0x71, 0x73, 0x68}, Offset: 0},
		{Name: "VMFS", Signature: []byte{'C', '0', 'W', '2', 'K', 'C', 'C', 0x00}, Offset: 0x1300},
		{Name: "VxFS", Signature: []byte{0xa5, 0x01, 0x00, 0x00}, Offset: 0x40},
		{Name: "UDF", Signature: []byte{0x01, 0x50, 0x4E, 0x41, 0x31, 0x33, 0x30, 0x31}, Offset: 0x4028},
		{Name: "UFS (FreeBSD)", Signature: []byte{0x19, 0x54, 0x01, 0x00}, Offset: 0x8000},
		{Name: "UFS (NetBSD)", Signature: []byte{0x19, 0x55, 0x01, 0x00}, Offset: 0x8000},
		{Name: "UFS (OpenBSD)", Signature: []byte{0x19, 0x56, 0x01, 0x00}, Offset: 0x8000},
		{Name: "VFAT", Signature: []byte{0x55, 0xaa}, Offset: 0x1fe},
		{Name: "XFS", Signature: []byte("XFSB"), Offset: 0},
		{Name: "ZFS", Signature: []byte{0x00, 0x4D, 0x5A, 0x93, 0x13, 0x41, 0x4A, 0x16}, Offset: 0},
	}

	buffer := make([]byte, 512)
	_, err := file.ReadAt(buffer, offset)
	if err != nil {
		log.Printf("Error reading partition data: %v", err)
		return "Unknown"
	}

	for _, fs := range fsList {
		if len(buffer) >= int(fs.Offset)+len(fs.Signature) && bytes.Equal(buffer[fs.Offset:fs.Offset+int64(len(fs.Signature))], fs.Signature) {
			return fs.Name
		}
	}

	extFsType := detectExtFilesystem(file, offset)
	if extFsType != "Unknown" {
		return extFsType
	}

	return "Unknown"
}

func detectExtFilesystem(file *os.File, offset int64) string {
	const superblockOffset = 0x400
	buffer := make([]byte, 0x70)

	_, err := file.ReadAt(buffer, offset+superblockOffset)
	if err != nil {
		return "Unknown"
	}

	magic := binary.LittleEndian.Uint16(buffer[0x38:0x3a])
	compatibleFeatures := binary.LittleEndian.Uint32(buffer[0x5c:0x60])

	if magic != 0xEF53 {
		return "Unknown"
	}

	if (compatibleFeatures & 0x40) == 0x40 {
		return "ext4"
	} else if (compatibleFeatures & 0x4) == 0x4 {
		return "ext3"
	} else {
		return "ext2"
	}
}

func isPrintable(b byte) bool {
	return b >= 32 && b <= 126
}

func printFirstNBytes(device string, numOfBytes int) error {
	file, err := os.Open(device)
	if err != nil {
		return err
	}
	defer file.Close()

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
		fmt.Printf("%08X  %-49s  |%s|\n", i, hexStr, charStr)
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
		fmt.Println("Running inside WSL!")
	}

	return WSL
}

func listDisks() {
	devices, err := os.ReadDir("/dev")
	if err != nil {
		fmt.Printf("Error reading /dev directory: %v\n", err)
		return
	}

	for _, device := range devices {
		devicePath := "/dev/" + device.Name()
		if deviceIsRealDisk(devicePath, false) {
			fmt.Println(devicePath)
		}
	}
}

func deviceIsRealDisk(device string, showPartitions bool) bool {
	isSd := strings.HasPrefix(device, "/dev/sd")
	isHd := strings.HasPrefix(device, "/dev/hd")
	isNvme := strings.HasPrefix(device, "/dev/nvme")

	// Check if the device name has a number (indicating a partition)
	if showPartitions {
		return (isSd || isHd || isNvme)
	}
	hasNumber := strings.IndexFunc(device, unicode.IsDigit) != -1

	return (isSd || isHd || isNvme) && !hasNumber
}

func readdisk(device, outputfile, compressionAlgorithm string) {
	// Open the disk device file
	disk, err := os.Open(device)
	if err != nil {
		fmt.Println("Failed to open Device: ", device)
		return
	}
	defer disk.Close()

	// Create a new file to write the data to
	output, err := os.Create(outputfile)
	if err != nil {
		fmt.Println("Failed to create output file: ", outputfile)
		return
	}
	defer output.Close()

	var compressedWriter io.Writer
	var zipWriter *zip.Writer

	switch compressionAlgorithm {
	case "gzip":
		compressedWriter = gzip.NewWriter(output)
	case "zlib":
		compressedWriter = zlib.NewWriter(output)
	case "bzip2":
		compressedWriter, err = bzip2.NewWriter(output, &bzip2.WriterConfig{})
	case "snappy":
		compressedWriter = snappy.NewWriter(output)
	case "zstd":
		compressedWriter, err = zstd.NewWriter(output)
	case "zip":
		zipWriter = zip.NewWriter(output)
		zipFile, err := zipWriter.Create("compressedData")
		if err != nil {
			fmt.Println("Failed to create zip entry:", err.Error())
			return
		}
		compressedWriter = zipFile
	default:
		fmt.Println("Unsupported compression algorithm:", compressionAlgorithm)
		return
	}

	if err != nil {
		fmt.Println("Failed to create compression writer: ", err.Error())
		return
	}

	fmt.Println("Writing to Image", outputfile)
	var count int = 0
	var byteCount = 16384
	// Use a buffer to read the data from the disk and write it to the file
	buf := make([]byte, byteCount)
	for {
		n, err := disk.Read(buf)
		if err != nil {
			break
		}

		_, err = compressedWriter.Write(buf[:n])
		if err != nil {
			fmt.Println("Failed to create compressed stream, ", err.Error())
		}
		count++
		output := count * byteCount
		if output%1048576 == 0 {
			fmt.Print("#")
		}
	}
	fmt.Println()
	fmt.Println("Written:", count*byteCount, "(", count, " Packets each ", byteCount, " bytes long )")

	if zipWriter != nil {
		err := zipWriter.Close()
		if err != nil {
			fmt.Println("Failed to close zip writer:", err.Error())
		}
	} else {
		compressedWriter.(io.WriteCloser).Close()
	}
}

func hasReadPermission(device string) bool {
	file, err := os.OpenFile(device, os.O_RDONLY, 0)
	if err != nil {
		return false
	}
	file.Close()
	return true
}

//go:build windows
// +build windows

package main

import (
	"compress/gzip"
	"fmt"
	"os"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	FILE_SHARE_READ                      = 0x1
	FILE_SHARE_WRITE                     = 0x2
	OPEN_EXISTING                        = 0x3
	GENERIC_READ                         = 0x80000000
	IOCTL_VOLUME_GET_VOLUME_DISK_EXTENTS = 0x00560000
	IOCTL_DISK_GET_DRIVE_GEOMETRY_EX     = 0x000700a0
	IOCTL_DISK_GET_DRIVE_LAYOUT_EX       = 0x00070050
)

type DiskGeometry struct {
	Cylinders         int64
	MediaType         uint32
	TracksPerCylinder uint32
	SectorsPerTrack   uint32
	BytesPerSector    uint32
}

type DiskGeometryEx struct {
	Geometry DiskGeometry
	DiskSize int64
	Data     [1]byte
}

type PartitionInformationEx struct {
	PartitionStyle   uint32
	StartingOffset   int64
	PartitionLength  int64
	PartitionNumber  uint32
	RewritePartition uint32
	Gpt              windows.GUID
	HiddenSectors    uint32
}

type DriveLayoutInformationEx struct {
	PartitionStyle uint32
	PartitionCount uint32
	PartitionEntry [128]PartitionInformationEx
}

func listPartitions(diskDevice string) {
	//this needs some work to pass the drive letter, also need to check if it actually works on windows :P
	listPartitionsWindows()
}

func listDisks() {
	listRealDisksWindows()
}

func listPartitionsWindows() {

	driveLetter := "C" // Change this to the desired drive letter
	diskNumber, err := driveLetterToDiskNumber(driveLetter)
	if err != nil {
		fmt.Println("Error converting drive letter to disk number:", err)
		return
	}

	// diskNumber := uint32(0) //0 = C:, 1 = D:, etc.

	hDisk, err := windows.CreateFile(
		windows.StringToUTF16Ptr(fmt.Sprintf(`\\.\PhysicalDrive%d`, diskNumber)),
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_EXISTING,
		0,
		0)

	if err != nil {
		fmt.Println("Error opening disk:", err)
		return
	}
	defer windows.CloseHandle(hDisk)

	var diskGeometry DiskGeometryEx
	err = windows.DeviceIoControl(hDisk, IOCTL_DISK_GET_DRIVE_GEOMETRY_EX, nil, 0, (*byte)(unsafe.Pointer(&diskGeometry)), uint32(unsafe.Sizeof(diskGeometry)), nil, nil)
	if err != nil {
		fmt.Println("Error getting disk geometry:", err)
		return
	}

	var driveLayout DriveLayoutInformationEx
	driveLayoutSize := uint32(unsafe.Sizeof(driveLayout) + 128*unsafe.Sizeof(driveLayout.PartitionEntry[0]))
	buffer := make([]byte, driveLayoutSize)
	err = windows.DeviceIoControl(hDisk, IOCTL_DISK_GET_DRIVE_LAYOUT_EX, nil, 0, &buffer[0], driveLayoutSize, nil, nil)
	if err != nil {
		fmt.Println("Error getting drive layout:", err)
		return
	}

	driveLayout = *(*DriveLayoutInformationEx)(unsafe.Pointer(&buffer[0]))

	fmt.Printf("Found %d partitions on disk %d:\n", driveLayout.PartitionCount, diskNumber)
	for i := uint32(0); i < driveLayout.PartitionCount; i++ {
		partition := driveLayout.PartitionEntry[i]
		fmt.Printf("Partition %d: Type: %d, StartingOffset: %d, PartitionLength: %d, HiddenSectors: %d\n", i+1, partition.PartitionStyle, partition.StartingOffset, partition.PartitionLength, partition.HiddenSectors)
	}
}

func driveLetterToDiskNumber(driveLetter string) (int, error) {
	driveLetter = strings.ToUpper(driveLetter)
	if len(driveLetter) != 1 || driveLetter[0] < 'A' || driveLetter[0] > 'Z' {
		return -1, fmt.Errorf("Invalid drive letter")
	}

	volumeName := fmt.Sprintf("\\\\.\\%s:", driveLetter)
	volumeHandle, err := windows.CreateFile(
		syscall.StringToUTF16Ptr(volumeName),
		GENERIC_READ,
		FILE_SHARE_READ|FILE_SHARE_WRITE,
		nil,
		OPEN_EXISTING,
		0,
		0)

	if err != nil {
		return -1, fmt.Errorf("Error opening volume: %s", err)
	}
	defer windows.CloseHandle(volumeHandle)

	type DiskExtent struct {
		DiskNumber     uint32
		StartingOffset int64
		ExtentLength   int64
	}
	type VolumeDiskExtents struct {
		NumberOfDiskExtents uint32
		Extents             [1]DiskExtent
	}

	var extents VolumeDiskExtents
	var bytesReturned uint32
	err = windows.DeviceIoControl(volumeHandle, IOCTL_VOLUME_GET_VOLUME_DISK_EXTENTS, nil, 0, (*byte)(unsafe.Pointer(&extents)), uint32(unsafe.Sizeof(extents)), &bytesReturned, nil)
	if err != nil {
		return -1, fmt.Errorf("Error getting volume disk extents: %s", err)
	}

	return int(extents.Extents[0].DiskNumber), nil
}

func listRealDisksWindows() {
	driveBits, err := windows.GetLogicalDrives()
	if err != nil {
		fmt.Printf("Failed to get logical drives: %v\n", err)
		return
	}

	for i := 0; i < 26; i++ {
		if driveBits&(1<<uint(i)) != 0 {
			driveLetter := string('A' + i)
			fmt.Printf("%s:\\\n", driveLetter)
		}
	}
}

func readdisk(device, outputfile string) {
	readdiskWindows(device, outputfile)
}

func readdiskWindows(device, outputfile string) {
	// Open the disk device file using the syscall package
	disk, err := syscall.CreateFile(
		syscall.StringToUTF16Ptr("\\\\.\\F:"), // Replace "F:" with the drive letter of the disk
		syscall.GENERIC_READ,
		syscall.FILE_SHARE_READ,
		nil,
		syscall.OPEN_EXISTING,
		syscall.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		// Handle error
	}
	defer syscall.CloseHandle(disk)

	// Create a new file to write the data to
	output, err := os.Create(outputfile)
	if err != nil {
		// Handle error
	}
	defer output.Close()

	// Create a gzip writer
	gzipWriter := gzip.NewWriter(output)
	defer gzipWriter.Close()

	// Use a buffer to read the data from the disk and write it to the file
	buf := make([]byte, 1024)
	for {
		var n uint32
		err := syscall.ReadFile(disk, buf, &n, nil)
		if err != nil {
			break
		}
		gzipWriter.Write(buf[:n])
	}
}

func printDiskBytes(diskDevice string, numOfBytes int) {
	fmt.Println("Windows unsupported for now")
}

func hasReadPermission(device string) bool {
	lpFileName, _ := syscall.UTF16PtrFromString(device)
	var sd *syscall.SECURITY_DESCRIPTOR

	err := syscall.GetFileSecurity(
		lpFileName,
		syscall.DACL_SECURITY_INFORMATION,
		(*byte)(unsafe.Pointer(sd)),
		0,
		&syscall.SECURITY_DESCRIPTOR_REVISION)
	if err != nil {
		return false
	}

	var genericMapping syscall.GENERIC_MAPPING
	genericMapping.GenericRead = syscall.FILE_GENERIC_READ
	genericMapping.GenericWrite = syscall.FILE_GENERIC_WRITE
	genericMapping.GenericExecute = syscall.FILE_GENERIC_EXECUTE
	genericMapping.GenericAll = syscall.FILE_ALL_ACCESS

	var privileges syscall.PRIVILEGE_SET
	var grantedAccess uint32
	var accessStatus bool

	err = syscall.AccessCheck(
		(*byte)(unsafe.Pointer(sd)),
		syscall.Token(nil),
		syscall.FILE_GENERIC_READ,
		&genericMapping,
		&privileges,
		uint32(unsafe.Sizeof(privileges)),
		&grantedAccess,
		&accessStatus)
	if err != nil || !accessStatus {
		return false
	}

	return true
}

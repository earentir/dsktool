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

func listPartitions(diskDevice string) {
	// Clean up input
	diskDevice = strings.TrimRight(strings.ToUpper(diskDevice), "\\/:")
	if len(diskDevice) != 1 || diskDevice[0] < 'A' || diskDevice[0] > 'Z' {
		fmt.Printf("Invalid drive letter: %s\n", diskDevice)
		return
	}

	diskNumber, err := driveLetterToDiskNumber(diskDevice)
	if err != nil {
		fmt.Printf("Error getting disk number: %v\n", err)
		return
	}

	physicalDrive := fmt.Sprintf("\\\\.\\PhysicalDrive%d", diskNumber)
	hDisk, err := windows.CreateFile(
		windows.StringToUTF16Ptr(physicalDrive),
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_EXISTING,
		0,
		0)

	if err != nil {
		if err == windows.ERROR_ACCESS_DENIED {
			fmt.Println("Access denied. Please run as administrator")
		} else {
			fmt.Printf("Error opening disk: %v\n", err)
		}
		return
	}
	defer windows.CloseHandle(hDisk)

	var diskGeometry DiskGeometryEx
	err = windows.DeviceIoControl(
		hDisk,
		IOCTL_DISK_GET_DRIVE_GEOMETRY_EX,
		nil,
		0,
		(*byte)(unsafe.Pointer(&diskGeometry)),
		uint32(unsafe.Sizeof(diskGeometry)),
		nil,
		nil)
	if err != nil {
		fmt.Printf("Error getting disk geometry: %v\n", err)
		return
	}

	var driveLayout DriveLayoutInformationEx
	driveLayoutSize := uint32(unsafe.Sizeof(driveLayout))
	buffer := make([]byte, driveLayoutSize)
	err = windows.DeviceIoControl(
		hDisk,
		IOCTL_DISK_GET_DRIVE_LAYOUT_EX,
		nil,
		0,
		&buffer[0],
		driveLayoutSize,
		nil,
		nil)
	if err != nil {
		fmt.Printf("Error getting drive layout: %v\n", err)
		return
	}

	driveLayout = *(*DriveLayoutInformationEx)(unsafe.Pointer(&buffer[0]))
	fmt.Printf("Found %d partitions on disk %d:\n", driveLayout.PartitionCount, diskNumber)
	for i := uint32(0); i < driveLayout.PartitionCount; i++ {
		partition := driveLayout.PartitionEntry[i]
		fmt.Printf("Partition %d: Type: %d, StartingOffset: %d, PartitionLength: %d, HiddenSectors: %d\n",
			i+1, partition.PartitionStyle, partition.StartingOffset, partition.PartitionLength, partition.HiddenSectors)
	}
}

func driveLetterToDiskNumber(driveLetter string) (int, error) {
	// Clean up the drive letter input
	driveLetter = strings.TrimRight(strings.ToUpper(driveLetter), "\\/:") // Remove trailing slashes and colon
	if len(driveLetter) != 1 || driveLetter[0] < 'A' || driveLetter[0] > 'Z' {
		return -1, fmt.Errorf("Invalid drive letter: %s", driveLetter)
	}

	// Format the path correctly for Windows API
	volumePath := fmt.Sprintf("\\\\.\\%s:", driveLetter)

	volumeHandle, err := windows.CreateFile(
		windows.StringToUTF16Ptr(volumePath),
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_EXISTING,
		0,
		0)

	if err != nil {
		if err == windows.ERROR_ACCESS_DENIED {
			return -1, fmt.Errorf("Access denied. Please run as administrator")
		}
		return -1, fmt.Errorf("Error opening volume: %v", err)
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

	err = windows.DeviceIoControl(
		volumeHandle,
		IOCTL_VOLUME_GET_VOLUME_DISK_EXTENTS,
		nil,
		0,
		(*byte)(unsafe.Pointer(&extents)),
		uint32(unsafe.Sizeof(extents)),
		&bytesReturned,
		nil)

	if err != nil {
		if err == windows.ERROR_MORE_DATA {
			// This is normal for volumes spanning multiple disks
			// For our purposes, we'll just return the first disk
			return int(extents.Extents[0].DiskNumber), nil
		}
		return -1, fmt.Errorf("Error getting volume disk extents: %v", err)
	}

	if extents.NumberOfDiskExtents == 0 {
		return -1, fmt.Errorf("No disk extents found for volume %s", driveLetter)
	}

	return int(extents.Extents[0].DiskNumber), nil
}

func listDisks() {
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

func readdisk(device, outputfile, compressionAlgorithm string) {
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

func printDiskBytes(diskDevice string, numOfBytes int, startIndex int64) {
	fmt.Println("Windows unsupported for now")
}

func hasReadPermission(device string) bool {
	// Handle default case
	if device == "." {
		device = `\\.\PhysicalDrive0`
	}

	h, err := windows.CreateFile(
		windows.StringToUTF16Ptr(device),
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		if err == windows.ERROR_ACCESS_DENIED {
			fmt.Println("Access denied. Please run with administrator privileges.")
		}
		return false
	}
	windows.CloseHandle(h)
	return true
}

// Function to check if running with admin privileges
func isAdmin() bool {
	var sid *windows.SID
	err := windows.AllocateAndInitializeSid(
		&windows.SECURITY_NT_AUTHORITY,
		2,
		windows.SECURITY_BUILTIN_DOMAIN_RID,
		windows.DOMAIN_ALIAS_RID_ADMINS,
		0, 0, 0, 0, 0, 0,
		&sid)
	if err != nil {
		return false
	}
	defer windows.FreeSid(sid)

	token := windows.Token(0)
	err = windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY, &token)
	if err != nil {
		return false
	}
	defer token.Close()

	// Handle both return values
	isMember, err := token.IsMember(sid)
	if err != nil {
		return false
	}
	return isMember
}

// Helper function to check and report admin status
func checkAdminStatus() bool {
	if !isAdmin() {
		fmt.Println("This program requires administrator privileges.")
		fmt.Println("Please right-click and select 'Run as administrator'.")
		return false
	}
	return true
}

package main

import (
	"fmt"
	"os"
	"time"
)

func benchFullTest(size, iterations int, dir string) {
	// Handle default case
	if dir == "." {
		// Use Windows system drive
		dir = `\\.\PhysicalDrive0`
	} else {
		// If it's a drive letter, convert it to physical drive path
		if len(dir) == 2 && dir[1] == ':' {
			driveLetter := dir[0]
			diskNumber, err := driveLetterToDiskNumber(string(driveLetter))
			if err != nil {
				fmt.Printf("Error getting disk number: %v\n", err)
				return
			}
			dir = fmt.Sprintf(`\\.\PhysicalDrive%d`, diskNumber)
		}
	}

	fmt.Printf("Testing with file size: %d MB\n", size)
	fmt.Printf("Testing on device: %s\n\n", dir)

	// Open the device once to check permissions
	testFile, err := openForAsyncIO(dir)
	if err != nil {
		fmt.Printf("Error opening device %s: %v\n", dir, err)
		fmt.Println("Please run with administrator privileges")
		return
	}
	testFile.Close()

	runTest("Sequential Read/Write", size*mb, iterations, dir, sequentialReadWrite)
	runTest("512K Blocks", size*mb, iterations, dir, func(f *os.File, size int) (time.Duration, time.Duration) {
		return blockReadWrite(f, size, 512*kb)
	})
	runTest("4K Blocks", size*mb, iterations, dir, func(f *os.File, size int) (time.Duration, time.Duration) {
		return blockReadWrite(f, size, 4*kb)
	})
	runTest("4KQD32", size*mb, iterations, dir, func(f *os.File, size int) (time.Duration, time.Duration) {
		return queuedBlockReadWrite(f, size, 4*kb, 32)
	})
}

func runTest(name string, size, iterations int, devicePath string, testFunc func(*os.File, int) (writeDuration, readDuration time.Duration)) {
	var totalWriteDuration, totalReadDuration time.Duration

	for i := 0; i < iterations; i++ {
		tmpFile, err := openForAsyncIO(devicePath)
		if err != nil {
			fmt.Printf("Failed to open device: %v\n", err)
			return
		}

		writeDuration, readDuration := testFunc(tmpFile, size)
		totalWriteDuration += writeDuration
		totalReadDuration += readDuration

		writeSpeed := float64(size) / writeDuration.Seconds() / mb
		readSpeed := float64(size) / readDuration.Seconds() / mb
		fmt.Printf("[%s] Test %d: Write speed: %.2f MB/s, Read speed: %.2f MB/s\n", name, i+1, writeSpeed, readSpeed)

		tmpFile.Close()
	}

	avgWriteSpeed := float64(size*iterations) / totalWriteDuration.Seconds() / mb
	avgReadSpeed := float64(size*iterations) / totalReadDuration.Seconds() / mb
	fmt.Printf("[%s] Average: Write speed: %.2f MB/s, Read speed: %.2f MB/s\n\n", name, avgWriteSpeed, avgReadSpeed)
}

package main

import (
	"fmt"
	"os"
	"time"
)

func benchFullTest(size, iterations int, dir string) {
	fmt.Printf("Testing with file size: %d MB\n", size)
	fmt.Printf("Testing on directory: %s\n\n", dir)

	runTest("Sequential Read/Write", size*mb, iterations, dir, sequentialReadWrite)
	runTest("512K Blocks", size*mb, iterations, dir, func(f *os.File, size int) (time.Duration, time.Duration) { return blockReadWrite(f, size, 512*kb) })
	runTest("4K Blocks", size*mb, iterations, dir, func(f *os.File, size int) (time.Duration, time.Duration) { return blockReadWrite(f, size, 4*kb) })
	runTest("4KQD32", size*mb, iterations, dir, func(f *os.File, size int) (time.Duration, time.Duration) {
		return queuedBlockReadWrite(f, size, 4*kb, 32)
	})
}

func runTest(name string, size, iterations int, dir string, testFunc func(*os.File, int) (writeDuration, readDuration time.Duration)) {
	var totalWriteDuration, totalReadDuration time.Duration

	for i := 0; i < iterations; i++ {
		tmpFile, err := os.CreateTemp(dir, "speedtest")
		if err != nil {
			fmt.Println("Failed to create temp file:", err)
			return
		}
		defer os.Remove(tmpFile.Name())

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

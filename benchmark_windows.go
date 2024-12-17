package main

import (
	"fmt"
	"math/rand"
	"os"
	"sync"
	"time"

	"golang.org/x/sys/windows"
)

func sequentialReadWrite(f *os.File, size int) (writeDuration, readDuration time.Duration) {
	buf := generateRandomData(size)

	// Write
	startWrite := time.Now()
	asyncWrite(f, buf, 0)
	writeDuration = time.Since(startWrite)

	// Read
	readBuf := make([]byte, size)
	startRead := time.Now()
	asyncRead(f, readBuf, 0)
	readDuration = time.Since(startRead)

	return
}

func blockReadWrite(f *os.File, size, blockSize int) (writeDuration, readDuration time.Duration) {
	numBlocks := size / blockSize

	// Write
	startWrite := time.Now()
	for i := 0; i < numBlocks; i++ {
		buf := generateRandomData(blockSize)
		asyncWrite(f, buf, int64(i*blockSize))
	}
	writeDuration = time.Since(startWrite)

	// Read
	readBuf := make([]byte, blockSize)
	startRead := time.Now()
	for i := 0; i < numBlocks; i++ {
		asyncRead(f, readBuf, int64(i*blockSize))
	}
	readDuration = time.Since(startRead)

	return
}

func enablePrivilege(privilegeName string) error {
	var token windows.Token
	currentProcess, err := windows.GetCurrentProcess()
	if err != nil {
		return fmt.Errorf("GetCurrentProcess: %v", err)
	}

	err = windows.OpenProcessToken(currentProcess, windows.TOKEN_ADJUST_PRIVILEGES|windows.TOKEN_QUERY, &token)
	if err != nil {
		return fmt.Errorf("OpenProcessToken: %v", err)
	}
	defer token.Close()

	var luid windows.LUID
	err = windows.LookupPrivilegeValue(nil, windows.StringToUTF16Ptr(privilegeName), &luid)
	if err != nil {
		return fmt.Errorf("LookupPrivilegeValue: %v", err)
	}

	privileges := windows.Tokenprivileges{
		PrivilegeCount: 1,
		Privileges: [1]windows.LUIDAndAttributes{{
			Luid:       luid,
			Attributes: windows.SE_PRIVILEGE_ENABLED,
		}},
	}

	err = windows.AdjustTokenPrivileges(token, false, &privileges, 0, nil, nil)
	if err != nil {
		return fmt.Errorf("AdjustTokenPrivileges: %v", err)
	}

	return nil
}

func openPhysicalDisk(diskNumber int) (windows.Handle, error) {
	fmt.Printf("Debug: Attempting to open disk %d\n", diskNumber)
	fmt.Printf("Debug: Current privileges:\n")

	token := windows.Token(0)
	err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY, &token)
	if err != nil {
		fmt.Printf("Debug: Failed to query process token: %v\n", err)
	} else {
		defer token.Close()
		fmt.Printf("Debug: Successfully got process token\n")
	}

	fmt.Printf("Debug: Attempting to enable SeManageVolumePrivilege\n")
	err = enablePrivilege("SeManageVolumePrivilege")
	if err != nil {
		fmt.Printf("Debug: Failed to enable SeManageVolumePrivilege: %v\n", err)
	} else {
		fmt.Printf("Debug: Successfully enabled SeManageVolumePrivilege\n")
	}

	path := fmt.Sprintf(`\\.\PhysicalDrive%d`, diskNumber)
	fmt.Printf("Debug: Opening disk at path: %s\n", path)
	fmt.Printf("Debug: Requested access: GENERIC_READ|GENERIC_WRITE\n")
	fmt.Printf("Debug: Requested share mode: FILE_SHARE_READ|FILE_SHARE_WRITE\n")

	handle, err := windows.CreateFile(
		windows.StringToUTF16Ptr(path),
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_NO_BUFFERING|windows.FILE_FLAG_OVERLAPPED,
		0)

	if err != nil {
		fmt.Printf("Debug: CreateFile failed: %v (Error code: %d)\n", err, err.(windows.Errno))
	} else {
		fmt.Printf("Debug: Successfully opened disk handle\n")
	}

	return handle, err
}

func queuedBlockReadWrite(f *os.File, size, blockSize, queueDepth int) (writeDuration, readDuration time.Duration) {
	fmt.Printf("\nDebug: Starting benchmark with size=%d, blockSize=%d, queueDepth=%d\n", size, blockSize, queueDepth)

	diskHandle, err := openPhysicalDisk(0)
	if err != nil {
		fmt.Printf("Debug: Failed to open physical disk: %v\n", err)
		return
	}
	fmt.Printf("Debug: Successfully got disk handle\n")
	defer windows.CloseHandle(diskHandle)

	numBlocks := size / blockSize
	var wg sync.WaitGroup

	blocks := make(chan int, numBlocks)
	errors := make(chan error, 1)

	// Pre-fill blocks channel
	for i := 0; i < numBlocks; i++ {
		blocks <- i
	}
	close(blocks)

	// Write phase - wait for all goroutines to be ready
	var startWg sync.WaitGroup
	startWg.Add(queueDepth)

	startWrite := time.Now()

	// Start worker goroutines
	for i := 0; i < queueDepth; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			// Create event for overlapped I/O
			event, err := windows.CreateEvent(nil, 1, 0, nil)
			if err != nil {
				fmt.Printf("Debug: Failed to create event: %v\n", err)
				return
			}
			defer windows.CloseHandle(event)

			// Align buffer to sector size
			alignedSize := (blockSize + 511) & ^511
			buf := make([]byte, alignedSize)

			// Signal ready
			startWg.Done()

			for blockNum := range blocks {
				rand.Read(buf)
				offset := int64(blockNum) * int64(alignedSize)

				fmt.Printf("Debug: Write - Block %d, Offset: %d\n", blockNum, offset)

				overlapped := windows.Overlapped{
					HEvent:     event,
					OffsetHigh: uint32(offset >> 32),
					Offset:     uint32(offset),
				}

				// Reset event before operation
				windows.ResetEvent(event)

				var bytesWritten uint32
				err := windows.WriteFile(diskHandle, buf, &bytesWritten, &overlapped)
				if err != nil && err != windows.ERROR_IO_PENDING {
					fmt.Printf("Debug: WriteFile failed - Block %d: %v (Error code: %d)\n",
						blockNum, err, err.(windows.Errno))
					select {
					case errors <- err:
					default:
					}
					continue
				}

				if err == windows.ERROR_IO_PENDING {
					// Wait for the event
					waitResult, err := windows.WaitForSingleObject(event, windows.INFINITE)
					if err != nil || waitResult != windows.WAIT_OBJECT_0 {
						fmt.Printf("Debug: Wait failed for write - Block %d: %v\n", blockNum, err)
						continue
					}

					err = windows.GetOverlappedResult(diskHandle, &overlapped, &bytesWritten, false)
					if err != nil {
						fmt.Printf("Debug: GetOverlappedResult failed for write - Block %d: %v\n",
							blockNum, err)
						select {
						case errors <- err:
						default:
						}
					}
				}
			}
		}()
	}

	wg.Wait()
	writeDuration = time.Since(startWrite)

	// Read phase
	readBlocks := make(chan int, numBlocks)
	for i := 0; i < numBlocks; i++ {
		readBlocks <- i
	}
	close(readBlocks)

	// Reset wait groups
	wg = sync.WaitGroup{}
	startWg = sync.WaitGroup{}
	startWg.Add(queueDepth)

	startRead := time.Now()

	for i := 0; i < queueDepth; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			// Create event for overlapped I/O
			event, err := windows.CreateEvent(nil, 1, 0, nil)
			if err != nil {
				fmt.Printf("Debug: Failed to create event: %v\n", err)
				return
			}
			defer windows.CloseHandle(event)

			// Align buffer to sector size
			alignedSize := (blockSize + 511) & ^511
			buf := make([]byte, alignedSize)

			// Signal ready
			startWg.Done()

			for blockNum := range readBlocks {
				offset := int64(blockNum) * int64(alignedSize)
				fmt.Printf("Debug: Read - Block %d, Offset: %d\n", blockNum, offset)

				overlapped := windows.Overlapped{
					HEvent:     event,
					OffsetHigh: uint32(offset >> 32),
					Offset:     uint32(offset),
				}

				// Reset event before operation
				windows.ResetEvent(event)

				var bytesRead uint32
				err := windows.ReadFile(diskHandle, buf, &bytesRead, &overlapped)
				if err != nil && err != windows.ERROR_IO_PENDING {
					fmt.Printf("Debug: ReadFile failed - Block %d: %v (Error code: %d)\n",
						blockNum, err, err.(windows.Errno))
					select {
					case errors <- err:
					default:
					}
					continue
				}

				if err == windows.ERROR_IO_PENDING {
					// Wait for the event
					waitResult, err := windows.WaitForSingleObject(event, windows.INFINITE)
					if err != nil || waitResult != windows.WAIT_OBJECT_0 {
						fmt.Printf("Debug: Wait failed for read - Block %d: %v\n", blockNum, err)
						continue
					}

					err = windows.GetOverlappedResult(diskHandle, &overlapped, &bytesRead, false)
					if err != nil {
						fmt.Printf("Debug: GetOverlappedResult failed for read - Block %d: %v\n",
							blockNum, err)
						select {
						case errors <- err:
						default:
						}
					}
				}
			}
		}()
	}

	wg.Wait()
	readDuration = time.Since(startRead)

	// Check for errors
	select {
	case err := <-errors:
		fmt.Fprintf(os.Stderr, "Error during benchmark: %v\n", err)
	default:
	}

	return
}

func generateRandomData(size int) []byte {
	data := make([]byte, size)
	rand.Read(data)
	return data
}

// Modified async functions to use panic/recover for internal errors
func asyncWrite(f *os.File, buf []byte, offset int64) error {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "Recovered from panic in asyncWrite: %v\n", r)
		}
	}()

	overlapped := windows.Overlapped{
		OffsetHigh: uint32(offset >> 32),
		Offset:     uint32(offset & 0xFFFFFFFF),
	}

	var bytesWritten uint32
	err := windows.WriteFile(windows.Handle(f.Fd()), buf, &bytesWritten, &overlapped)
	if err != nil && err != windows.ERROR_IO_PENDING {
		return err
	}

	if err == windows.ERROR_IO_PENDING {
		err = windows.GetOverlappedResult(windows.Handle(f.Fd()), &overlapped, &bytesWritten, true)
	}

	return err
}

func asyncRead(f *os.File, buf []byte, offset int64) error {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "Recovered from panic in asyncRead: %v\n", r)
		}
	}()

	overlapped := windows.Overlapped{
		OffsetHigh: uint32(offset >> 32),
		Offset:     uint32(offset & 0xFFFFFFFF),
	}

	var bytesRead uint32
	err := windows.ReadFile(windows.Handle(f.Fd()), buf, &bytesRead, &overlapped)
	if err != nil && err != windows.ERROR_IO_PENDING {
		return err
	}

	if err == windows.ERROR_IO_PENDING {
		err = windows.GetOverlappedResult(windows.Handle(f.Fd()), &overlapped, &bytesRead, true)
	}

	return err
}

func openForAsyncIO(path string) (*os.File, error) {
	pathPtr := windows.StringToUTF16Ptr(path)
	handle, err := windows.CreateFile(
		pathPtr,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_OVERLAPPED|windows.FILE_FLAG_NO_BUFFERING,
		0,
	)

	if err != nil {
		return nil, err
	}

	return os.NewFile(uintptr(handle), path), nil
}

//go:build darwin

package main

import (
	"math/rand"
	"os"
	"sync"
	"time"
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

func queuedBlockReadWrite(f *os.File, size, blockSize, queueDepth int) (writeDuration, readDuration time.Duration) {
	numBlocks := size / blockSize
	ch := make(chan bool, queueDepth)
	var wg sync.WaitGroup

	// Write
	startWrite := time.Now()
	for i := 0; i < numBlocks; i++ {
		buf := generateRandomData(blockSize)
		ch <- true
		wg.Add(1)
		go func(i int, b []byte) {
			defer wg.Done()
			defer func() { <-ch }()
			asyncWrite(f, b, int64(i*blockSize))
		}(i, buf)
	}
	wg.Wait()
	writeDuration = time.Since(startWrite)

	// Read
	startRead := time.Now()
	for i := 0; i < numBlocks; i++ {
		readBuf := make([]byte, blockSize)
		ch <- true
		wg.Add(1)
		go func(i int, b []byte) {
			defer wg.Done()
			defer func() { <-ch }()
			asyncRead(f, b, int64(i*blockSize))
		}(i, readBuf)
	}
	wg.Wait()
	readDuration = time.Since(startRead)

	return
}

func generateRandomData(size int) []byte {
	data := make([]byte, size)
	rand.Read(data)
	return data
}

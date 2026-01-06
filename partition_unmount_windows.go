//go:build windows

package main

import "fmt"

// unmountPartition is a stub for Windows
func unmountPartition(diskPath string, part PartitionInfo) error {
	return fmt.Errorf("unmounting partitions is not supported on Windows")
}

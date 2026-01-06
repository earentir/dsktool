//go:build windows

package main

import "fmt"

// createPartition is a stub for Windows
func createPartition(diskPath string, unusedPart PartitionInfo, fields []FormField) error {
	return fmt.Errorf("creating partitions is not supported on Windows")
}

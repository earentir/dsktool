//go:build windows

package main

import (
	"fmt"
)

// deletePartition deletes a partition from the disk (Windows stub)
func deletePartition(diskPath string, part PartitionInfo) error {
	return fmt.Errorf("partition deletion not yet implemented for Windows")
}

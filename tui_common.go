package main

import (
	"fmt"
)

// capturePartitionOutput captures the output from listPartitions using the safe version
func capturePartitionOutput(diskPath string) string {
	output, err := listPartitionsSafe(diskPath)
	if err != nil {
		return fmt.Sprintf("Error reading partitions: %v\n", err)
	}
	return output
}

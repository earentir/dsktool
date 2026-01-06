//go:build windows

package main

import (
	"fmt"
)

// listPartitionsSafe is a version of listPartitions that returns errors instead of calling log.Fatalf
func listPartitionsSafe(diskDevice string) (string, error) {
	// Windows partition listing is not yet implemented in the safe version
	return "", fmt.Errorf("partition listing not yet implemented for Windows in TUI mode")
}

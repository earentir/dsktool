//go:build windows

package main

import (
	"fmt"
)

// getPartitionsDataDirectPlatform gets partition data directly from disk
func getPartitionsDataDirectPlatform(diskPath string) ([]PartitionInfo, error) {
	// Windows implementation not yet available
	return nil, fmt.Errorf("direct partition reading not yet implemented for Windows")
}

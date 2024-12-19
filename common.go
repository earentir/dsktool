package main

import (
	"fmt"
	"math"
	"os"
)

func isPrintable(b byte) bool {
	return b >= 32 && b <= 126
}

// Exit if we don't have permission to read the device
func checkForPerms(deviceToRead string) {
	if !hasReadPermission(deviceToRead) {
		fmt.Printf("No permission to read the device: %s, try with elevated priviledges\n", deviceToRead)
		os.Exit(13)
	}
}

func formatBytes[T dataSizeNumber](bytes T) string {
	byteCount := uint64(bytes)

	// Handle negative values by treating them as zero
	if byteCount == uint64(math.MaxInt64+1) { // Handling overflow for unsigned
		byteCount = 0
	}

	// Iterate through units to find the appropriate one
	var value float64
	var unit string
	for _, u := range units {
		if byteCount >= u.Threshold {
			value = float64(byteCount) / float64(u.Threshold)
			unit = u.Name
			break
		}
	}

	// If no unit matched, default to bytes
	if unit == "" {
		value = float64(byteCount)
		unit = "bytes"
	}

	// Determine if the value is an integer
	if value == math.Trunc(value) {
		return fmt.Sprintf("%.0f %s", value, unit)
	}
	return fmt.Sprintf("%.1f %s", value, unit)
}

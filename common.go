package main

import (
	"fmt"
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

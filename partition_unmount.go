//go:build !windows

package main

import (
	"fmt"
	"os/exec"
	"strings"
)

// unmountPartition unmounts a specific partition using diskutil (macOS) or umount (Linux)
func unmountPartition(diskPath string, part PartitionInfo) error {
	// Construct the partition device path
	// On macOS: /dev/rdisk1 -> /dev/disk1s1 (use disk, not rdisk for unmount)
	// On Linux: /dev/sda -> /dev/sda1
	var partitionPath string

	if strings.Contains(diskPath, "rdisk") {
		// Convert rdisk to disk for unmounting
		diskPathForUnmount := strings.Replace(diskPath, "/dev/rdisk", "/dev/disk", 1)
		partitionPath = fmt.Sprintf("%ss%d", diskPathForUnmount, part.Number)
	} else if strings.Contains(diskPath, "disk") {
		// Already using disk format
		partitionPath = fmt.Sprintf("%ss%d", diskPath, part.Number)
	} else {
		// Linux format: /dev/sda -> /dev/sda1
		partitionPath = fmt.Sprintf("%s%d", diskPath, part.Number)
	}

	// Check if partition is mounted
	if !part.Mounted {
		return fmt.Errorf("partition is not mounted")
	}

	// Unmount using platform-specific command
	return unmountPartitionPlatform(partitionPath)
}

// unmountPartitionPlatform performs the actual unmount operation
func unmountPartitionPlatform(partitionPath string) error {
	// Try diskutil unmount first (macOS)
	cmd := exec.Command("diskutil", "unmount", partitionPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// If diskutil fails, try umount (Linux)
		cmd = exec.Command("umount", partitionPath)
		output, err = cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to unmount %s: %s", partitionPath, string(output))
		}
	}

	return nil
}

//go:build linux

package main

// getDiskType detects the type of disk (physical, synthesized, image, etc.)
func getDiskType(devPath string) string {
	// On Linux, most disks are physical
	// We could check /sys/block/.../removable to detect external drives
	// For now, default to physical
	return "physical"
}

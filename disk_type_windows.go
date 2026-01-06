//go:build windows

package main

// getDiskType detects the type of disk (physical, synthesized, image, etc.)
func getDiskType(devPath string) string {
	// On Windows, most disks are physical
	// Could use WMI or other APIs to detect disk types
	return "physical"
}

package main

// DiskInfo represents information about a disk
type DiskInfo struct {
	Path      string
	Size      int64  // Size in bytes, 0 if unavailable
	SizeStr   string // Formatted size string
	MountInfo string // Mount point and filesystem info
	Mounted   bool   // Whether the disk is mounted
	DiskType  string // Type: "physical", "synthesized", "image", "unknown"
}

// getDiskListData returns structured disk information
// Platform-specific implementations in disks_linux.go, disks_darwin.go, disks_windows.go
func getDiskListData() []DiskInfo {
	return getDiskListDataPlatform()
}

package main

const (
	FILE_SHARE_READ                      = 0x1
	FILE_SHARE_WRITE                     = 0x2
	OPEN_EXISTING                        = 0x3
	GENERIC_READ                         = 0x80000000
	IOCTL_VOLUME_GET_VOLUME_DISK_EXTENTS = 0x00560000
	IOCTL_DISK_GET_DRIVE_GEOMETRY_EX     = 0x000700a0
	IOCTL_DISK_GET_DRIVE_LAYOUT_EX       = 0x00070050
)

type DiskGeometry struct {
	Cylinders         int64
	MediaType         uint32
	TracksPerCylinder uint32
	SectorsPerTrack   uint32
	BytesPerSector    uint32
}

type DiskGeometryEx struct {
	Geometry DiskGeometry
	DiskSize int64
	Data     [1]byte
}

type PartitionInformationEx struct {
	PartitionStyle   uint32
	StartingOffset   int64
	PartitionLength  int64
	PartitionNumber  uint32
	RewritePartition uint32
	Gpt              windows.GUID
	HiddenSectors    uint32
}

type DriveLayoutInformationEx struct {
	PartitionStyle uint32
	PartitionCount uint32
	PartitionEntry [128]PartitionInformationEx
}

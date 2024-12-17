package main

import (
	"golang.org/x/sys/windows"
)

const (
	IOCTL_DISK_GET_DRIVE_GEOMETRY_EX     = 0x000700A0
	IOCTL_DISK_GET_DRIVE_LAYOUT_EX       = 0x00070050
	IOCTL_VOLUME_GET_VOLUME_DISK_EXTENTS = 0x00560000
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

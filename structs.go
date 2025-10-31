package main

var (
	sectorSize uint64
	appversion = "0.4.31"
)

const (
	kb = 1 << 10
	mb = 1 << 20
	gb = 1 << 30
	tb = 1 << 40
	pb = 1 << 50
)

const (
	partitionTmpl = `
Disk           : {{.Disk}} ({{.DiskType}})
Partition Name : {{.PartitionName}}
FileSystem     : {{.Filesystem}}
TypeGUID       : {{.TypeGUIDStr}}
UniqueGUID     : {{.UniqueGUIDStr}}
Sector Size    : {{.SectorSize}} bytes
FirstLBA       : {{.Partition.FirstLBA}}
LastLBA        : {{.Partition.LastLBA}}
Total Sectors  : {{.TotalSectors}}
Total Size     : {{.Total}}
`
)

// DataSizeNumber is a type constraint that allows any signed or unsigned integer type.
type dataSizeNumber interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 |
		~uintptr
}

// Unit represents a data size unit with its name and threshold.
type Unit struct {
	Name      string
	Threshold uint64
}

// Predefined units in ascending order.
var units = []Unit{
	{"PB", pb},
	{"TB", tb},
	{"GB", gb},
	{"MB", mb},
	{"KB", kb},
	{"bytes", 1},
}

// Common partition structures shared across all platforms
type gptHeader struct {
	Signature           [8]byte
	Revision            [4]byte
	HeaderSize          uint32
	CRC32               uint32
	_                   [4]byte
	CurrentLBA          uint64
	BackupLBA           uint64
	FirstUsableLBA      uint64
	LastUsableLBA       uint64
	DiskGUID            [16]byte
	PartitionEntryLBA   uint64
	NumPartEntries      uint32
	PartEntrySize       uint32
	PartEntryArrayCRC32 uint32
}

type gptPartition struct {
	TypeGUID       [16]byte
	UniqueGUID     [16]byte
	FirstLBA       uint64
	LastLBA        uint64
	AttributeFlags uint64
	PartitionName  [72]byte
}

type gptPartitionDisplay struct {
	Disk          string
	DiskType      string
	PartitionName string
	Partition     gptPartition
	Name          string
	Filesystem    string
	TotalSectors  uint64
	SectorSize    uint64
	Total         string
	TypeGUIDStr   string
	UniqueGUIDStr string
}

type mbrPartition struct {
	Status      uint8
	_           [3]byte
	Type        uint8
	_           [3]byte
	FirstSector uint32
	Sectors     uint32
}

type mbrStruct struct {
	_          [446]byte
	Partitions [4]mbrPartition
	Signature  uint16
}

type fileSystemStruct struct {
	Name      string
	Signature []byte
	Offset    int64
}

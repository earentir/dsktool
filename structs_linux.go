package main

const (
	BLKGETSIZE64 = 0x80081272

	red   = "\033[31m"
	blink = "\033[5m"
	reset = "\033[0m"

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

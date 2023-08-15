package main

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
	Partition     gptPartition
	Name          string
	Filesystem    string
	TotalSectors  uint64
	SectorSize    uint64
	Total         uint64
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

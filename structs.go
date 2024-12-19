package main

var (
	sectorSize uint64
	appversion = "0.4.29"
)

const (
	kb = 1 << 10
	mb = 1 << 20
	gb = 1 << 30
	tb = 1 << 40
	pb = 1 << 50
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

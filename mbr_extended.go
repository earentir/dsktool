package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

// isExtendedType checks if a partition type is an extended partition type
func isExtendedType(t byte) bool {
	switch t {
	case 0x05, 0x0F, 0x85:
		return true
	default:
		return false
	}
}

// parseMBREntryFromBytes parses an MBR entry from raw bytes
func parseMBREntryFromBytes(b []byte) mbrPartition {
	return mbrPartition{
		Status:      b[0],
		Type:        b[4],
		FirstSector: binary.LittleEndian.Uint32(b[8:12]),
		Sectors:     binary.LittleEndian.Uint32(b[12:16]),
	}
}

// readEBRChain reads the extended boot record chain to find logical partitions
func readEBRChain(file *os.File, sizeBytes int64, sectorSize uint64, baseLBA uint32) ([]mbrPartition, error) {
	var logicalPartitions []mbrPartition
	nextEBR := uint64(baseLBA)
	maxHops := 128

	for hops := 0; hops < maxHops; hops++ {
		buf := make([]byte, sectorSize)
		_, err := file.ReadAt(buf, int64(nextEBR)*int64(sectorSize))
		if err != nil {
			return nil, fmt.Errorf("read EBR at LBA %d failed: %w", nextEBR, err)
		}

		if len(buf) < 512 || buf[510] != 0x55 || buf[511] != 0xAA {
			return nil, fmt.Errorf("EBR signature missing at LBA %d", nextEBR)
		}

		entries := buf[446 : 446+32]
		e1 := parseMBREntryFromBytes(entries[0:16])
		e2 := parseMBREntryFromBytes(entries[16:32])

		// First entry is the logical partition
		if e1.Type != 0x00 && e1.Sectors != 0 {
			// Convert relative LBA to absolute
			startLBA := nextEBR + uint64(e1.FirstSector)
			size := uint64(e1.Sectors)
			endLBA := startLBA + size - 1

			// Validate bounds if we know the disk size
			if sizeBytes > 0 {
				maxLBA := uint64(sizeBytes) / uint64(sectorSize)
				if startLBA <= maxLBA && endLBA <= maxLBA {
					logicalPartitions = append(logicalPartitions, mbrPartition{
						Status:      e1.Status,
						Type:        e1.Type,
						FirstSector: uint32(startLBA),
						Sectors:      uint32(size),
					})
				}
			} else {
				logicalPartitions = append(logicalPartitions, mbrPartition{
					Status:      e1.Status,
					Type:        e1.Type,
					FirstSector: uint32(startLBA),
					Sectors:      uint32(size),
				})
			}
		}

		// Second entry points to next EBR or is empty
		if e2.Type == 0x00 || e2.Sectors == 0 {
			break
		}
		if !isExtendedType(e2.Type) {
			break
		}
		nextEBR = uint64(baseLBA) + uint64(e2.FirstSector)
	}

	return logicalPartitions, nil
}

var _ io.ReaderAt

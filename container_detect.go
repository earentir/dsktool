package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

// ContainerType represents detected container/volume manager types
type ContainerType string

const (
	ContainerLUKS   ContainerType = "LUKS"
	ContainerLVM2PV ContainerType = "LVM2PV"
	ContainerMDRAID ContainerType = "MDRAID"
)

// ContainerInfo holds information about a detected container
type ContainerInfo struct {
	Type        ContainerType
	OffsetBytes int64
	Metadata    map[string]string
}

// detectLUKS detects LUKS encryption container
func detectLUKS(file *os.File, offset int64) (*ContainerInfo, error) {
	buf := make([]byte, 64)
	_, err := file.ReadAt(buf, offset)
	if err != nil {
		return nil, err
	}

	if len(buf) < 8 || !bytes.Equal(buf[0:6], []byte{'L', 'U', 'K', 'S', 0xBA, 0xBE}) {
		return nil, fmt.Errorf("no luks magic")
	}

	ver := binary.BigEndian.Uint16(buf[6:8])
	conf := "high"
	notes := fmt.Sprintf("LUKS version %d", ver)
	if ver != 1 && ver != 2 {
		conf = "medium"
		notes = fmt.Sprintf("LUKS magic found but version %d unexpected", ver)
	}

	return &ContainerInfo{
		Type:        ContainerLUKS,
		OffsetBytes: offset,
		Metadata: map[string]string{
			"confidence": conf,
			"version":    fmt.Sprintf("%d", ver),
			"notes":      notes,
		},
	}, nil
}

// detectLVM2PV detects LVM2 Physical Volume
func detectLVM2PV(file *os.File, offset int64, sectorSize uint64) (*ContainerInfo, error) {
	// Check multiple offsets like parttool does
	offsets := []int64{0, int64(sectorSize), 4 * int64(sectorSize), 8 * int64(sectorSize), 4096}

	for _, off := range offsets {
		if off < 0 {
			continue
		}
		buf := make([]byte, 512)
		_, err := file.ReadAt(buf, offset+off)
		if err != nil {
			continue
		}

		hasLabelOne := bytes.Contains(buf, []byte("LABELONE"))
		hasLVM2 := bytes.Contains(buf, []byte("LVM2 001"))

		if hasLabelOne && hasLVM2 {
			return &ContainerInfo{
				Type:        ContainerLVM2PV,
				OffsetBytes: offset + off,
				Metadata: map[string]string{
					"confidence": "high",
					"notes":      "LABELONE and LVM2 001 markers found",
				},
			}, nil
		}
		if hasLabelOne {
			return &ContainerInfo{
				Type:        ContainerLVM2PV,
				OffsetBytes: offset + off,
				Metadata: map[string]string{
					"confidence": "medium",
					"notes":      "LABELONE marker found",
				},
			}, nil
		}
	}
	return nil, fmt.Errorf("no lvm2 pv")
}

// detectMDRAID detects Linux MD RAID
func detectMDRAID(file *os.File, offset int64, sizeBytes int64, maxScanBytes int64) (*ContainerInfo, error) {
	if maxScanBytes <= 0 {
		maxScanBytes = 8 * 1024 * 1024 // 8 MiB default
	}

	candidates := []int64{4096, 8192}
	if sizeBytes > 0 {
		end1 := sizeBytes - 65536
		end2 := sizeBytes - 131072
		if end1 > 0 {
			candidates = append(candidates, end1)
		}
		if end2 > 0 {
			candidates = append(candidates, end2)
		}
	}

	for _, off := range candidates {
		if off < 0 {
			continue
		}
		if maxScanBytes > 0 && sizeBytes > 0 {
			if off < sizeBytes-maxScanBytes && off > maxScanBytes {
				continue
			}
		}

		buf := make([]byte, 4096)
		_, err := file.ReadAt(buf, offset+off)
		if err != nil {
			continue
		}

		if len(buf) < 4 {
			continue
		}
		mLE := binary.LittleEndian.Uint32(buf[0:4])
		mBE := binary.BigEndian.Uint32(buf[0:4])
		if mLE != 0xA92B4EFC && mBE != 0xA92B4EFC {
			continue
		}

		conf := "medium"
		notes := "mdraid magic found (detect-only)"
		if len(buf) >= 8 {
			conf = "high"
			notes = "mdraid magic found with basic validation"
		}

		return &ContainerInfo{
			Type:        ContainerMDRAID,
			OffsetBytes: offset + off,
			Metadata: map[string]string{
				"confidence": conf,
				"notes":      notes,
			},
		}, nil
	}

	return nil, fmt.Errorf("no mdraid")
}

// detectContainers scans for container/volume manager signatures
func detectContainers(file *os.File, offset int64, sizeBytes int64, sectorSize uint64) []ContainerInfo {
	var containers []ContainerInfo

	// Try LUKS at offset
	if luks, err := detectLUKS(file, offset); err == nil {
		containers = append(containers, *luks)
	}

	// Try LVM2PV at offset
	if lvm, err := detectLVM2PV(file, offset, sectorSize); err == nil {
		containers = append(containers, *lvm)
	}

	// Try MDRAID (scans multiple locations)
	if mdraid, err := detectMDRAID(file, offset, sizeBytes, 8*1024*1024); err == nil {
		containers = append(containers, *mdraid)
	}

	return containers
}

var _ io.ReaderAt

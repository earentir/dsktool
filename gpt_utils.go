package main

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"unicode/utf16"
)

// guidToString formats a GUID byte array into the standard string format
func guidToString(b []byte) string {
	if len(b) < 16 {
		return ""
	}
	d1 := binary.LittleEndian.Uint32(b[0:4])
	d2 := binary.LittleEndian.Uint16(b[4:6])
	d3 := binary.LittleEndian.Uint16(b[6:8])
	return fmt.Sprintf("%08x-%04x-%04x-%02x%02x-%02x%02x%02x%02x%02x%02x",
		d1, d2, d3,
		b[8], b[9],
		b[10], b[11], b[12], b[13], b[14], b[15],
	)
}

// decodeUTF16LE decodes UTF-16LE encoded partition names
func decodeUTF16LE(b []byte) string {
	if len(b)%2 != 0 {
		b = b[:len(b)-1]
	}
	u16 := make([]uint16, 0, len(b)/2)
	for i := 0; i < len(b); i += 2 {
		v := binary.LittleEndian.Uint16(b[i : i+2])
		if v == 0 {
			break
		}
		u16 = append(u16, v)
	}
	return string(utf16.Decode(u16))
}

// isAllZero checks if a byte slice is all zeros
func isAllZero(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}

// validateGPTHeaderCRC validates the CRC32 of a GPT header
func validateGPTHeaderCRC(headerBytes []byte, headerSize uint32) error {
	if len(headerBytes) < int(headerSize) {
		return fmt.Errorf("header too small for validation")
	}

	origCRC := binary.LittleEndian.Uint32(headerBytes[16:20])

	// Create a copy with CRC field zeroed
	tmp := make([]byte, headerSize)
	copy(tmp, headerBytes[:headerSize])
	for i := 16; i < 20; i++ {
		tmp[i] = 0
	}

	calculatedCRC := crc32.ChecksumIEEE(tmp)
	if calculatedCRC != origCRC {
		return fmt.Errorf("GPT header CRC mismatch: calculated 0x%08X, expected 0x%08X", calculatedCRC, origCRC)
	}

	return nil
}

// validateGPTEntriesCRC validates the CRC32 of GPT partition entries
func validateGPTEntriesCRC(entries []byte, expectedCRC uint32) error {
	calculatedCRC := crc32.ChecksumIEEE(entries)
	if calculatedCRC != expectedCRC {
		return fmt.Errorf("GPT entries CRC mismatch: calculated 0x%08X, expected 0x%08X", calculatedCRC, expectedCRC)
	}
	return nil
}

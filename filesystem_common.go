package main

import (
	"bytes"
	"encoding/binary"
	"log"
	"os"
)

// filesystemList contains all known filesystem signatures for detection
var filesystemList = []fileSystemStruct{
	{Name: "Amiga FFS", Signature: []byte{0x44, 0x4F, 0x53}, Offset: 0x3400},
	{Name: "APFS", Signature: []byte("NXSB"), Offset: 0},
	{Name: "AUFS (SunOS)", Signature: []byte{0x2a, 0x2a, 0x2a, 0x14}, Offset: 0},
	{Name: "Btrfs", Signature: []byte("_BHRfS_M"), Offset: 0x40},
	{Name: "BeFS (BeOS)", Signature: []byte{0x69, 0x19, 0x01, 0x00}, Offset: 0x414},
	{Name: "CramFS", Signature: []byte{0x28, 0xcd, 0x3d, 0x45}, Offset: 0},
	{Name: "CramFS (swapped)", Signature: []byte{0x45, 0x3d, 0xcd, 0x28}, Offset: 0},
	{Name: "EFS (Ext2 Encrypted)", Signature: []byte{0x53, 0xef, 0x01, 0x00}, Offset: 0x438},
	{Name: "exFAT", Signature: []byte{0x45, 0x58, 0x46, 0x41, 0x54}, Offset: 3},
	{Name: "FAT32", Signature: []byte{0x55, 0xaa}, Offset: 0x1fe},
	{Name: "FAT12/16", Signature: []byte{0x55, 0xaa}, Offset: 0x1fe},
	{Name: "F2FS", Signature: []byte{0xF2, 0xF5, 0x20, 0x10}, Offset: 0x400},
	{Name: "HAMMER (DragonFly BSD)", Signature: []byte{0x34, 0xC1, 0x03, 0x49}, Offset: 0x200},
	{Name: "HAMMER2 (DragonFly BSD)", Signature: []byte("H2"), Offset: 0x08},
	{Name: "HPFS", Signature: []byte{0xf8, 0x2a, 0x2b, 0x01}, Offset: 0},
	{Name: "HFS", Signature: []byte{'B', 'D', 0x00, 0x01}, Offset: 0x400},
	{Name: "HFS+", Signature: []byte{'H', '+', 0x00, 0x04}, Offset: 0x400},
	{Name: "ISO9660", Signature: []byte("CD001"), Offset: 0x8001},
	{Name: "JFS", Signature: []byte("JFS1"), Offset: 0x8004},
	{Name: "Swap (Linux)", Signature: []byte("SWAPSPACE2"), Offset: 0x40C0},
	{Name: "LVM", Signature: []byte("LVM2 001"), Offset: 0x218},
	{Name: "LVM", Signature: []byte("LABELONE"), Offset: 0x204},
	{Name: "Minix (30 char)", Signature: []byte{0x18, 0x03, 0x78, 0x56}, Offset: 0x410},
	{Name: "Minix (62 char)", Signature: []byte{0x18, 0x04, 0x78, 0x56}, Offset: 0x410},
	{Name: "Minix v2 (30 char)", Signature: []byte{0x24, 0x05, 0x19, 0x05}, Offset: 0x410},
	{Name: "Minix v2 (62 char)", Signature: []byte{0x24, 0x05, 0x19, 0x08}, Offset: 0x410},
	{Name: "NILFS2", Signature: []byte{0x34, 0x34, 0x5E, 0x1C}, Offset: 0x400},
	{Name: "NTFS", Signature: []byte("NTFS"), Offset: 3},
	{Name: "OCFS2", Signature: []byte("OCFSV2"), Offset: 0x2000},
	{Name: "QNX6", Signature: []byte("QNX6"), Offset: 0x4},
	{Name: "ReiserFS", Signature: []byte{0x34, 0x34}, Offset: 0x10034},
	{Name: "Reiser4", Signature: []byte{0x4A, 0x4A}, Offset: 0x10034},
	{Name: "RomFS", Signature: []byte("-rom1fs-"), Offset: 0},
	{Name: "SkyFS (Haiku)", Signature: []byte{0x79, 0x30, 0x33, 0x01}, Offset: 0x414},
	{Name: "SysV", Signature: []byte{0xfd, 0x37, 0x59, 0x5F}, Offset: 0},
	{Name: "SquashFS", Signature: []byte{0x73, 0x71, 0x73, 0x68}, Offset: 0},
	{Name: "VMFS", Signature: []byte{'C', '0', 'W', '2', 'K', 'C', 'C', 0x00}, Offset: 0x1300},
	{Name: "VxFS", Signature: []byte{0xa5, 0x01, 0x00, 0x00}, Offset: 0x40},
	{Name: "UDF", Signature: []byte{0x01, 0x50, 0x4E, 0x41, 0x31, 0x33, 0x30, 0x31}, Offset: 0x4028},
	{Name: "UFS (FreeBSD)", Signature: []byte{0x19, 0x54, 0x01, 0x00}, Offset: 0x8000},
	{Name: "UFS (NetBSD)", Signature: []byte{0x19, 0x55, 0x01, 0x00}, Offset: 0x8000},
	{Name: "UFS (OpenBSD)", Signature: []byte{0x19, 0x56, 0x01, 0x00}, Offset: 0x8000},
	{Name: "VFAT", Signature: []byte{0x55, 0xaa}, Offset: 0x1fe},
	{Name: "XFS", Signature: []byte("XFSB"), Offset: 0},
	{Name: "ZFS", Signature: []byte{0x00, 0x4D, 0x5A, 0x93, 0x13, 0x41, 0x4A, 0x16}, Offset: 0},
	// New Filesystems
	{Name: "Microsoft Basic Data", Signature: []byte{0xEB, 0x52, 0x90}, Offset: 0}, // Boot sector signature
	{Name: "AFS", Signature: []byte("AFS"), Offset: 0x100},
	{Name: "Apple UFS", Signature: []byte{0x19, 0x57, 0x01, 0x00}, Offset: 0x8000},
	{Name: "EROFS", Signature: []byte("E0F5"), Offset: 0x400}, // Enhanced Read-Only File System
	{Name: "FUSE GRPC", Signature: []byte("GRPC"), Offset: 0},
	{Name: "GFS/GFS2", Signature: []byte("GFSL"), Offset: 0x400},
	{Name: "UBIFS", Signature: []byte{0x31, 0x18, 0x10, 0x06}, Offset: 0},
	{Name: "YAFFS2", Signature: []byte("YFSS"), Offset: 0},
	{Name: "NOVA", Signature: []byte("NOVA"), Offset: 0x200},
	{Name: "JFFS2", Signature: []byte{0x85, 0x19}, Offset: 0},
	{Name: "LogFS", Signature: []byte("LOGFS"), Offset: 0},
}

// detectFileSystem detects the filesystem type by reading and matching signatures
func detectFileSystem(file *os.File, offset int64) string {
	buffer := make([]byte, 512)
	_, err := file.ReadAt(buffer, offset)
	if err != nil {
		log.Printf("Error reading partition data: %v", err)
		return "Unknown"
	}

	for _, fs := range filesystemList {
		if len(buffer) >= int(fs.Offset)+len(fs.Signature) && bytes.Equal(buffer[fs.Offset:fs.Offset+int64(len(fs.Signature))], fs.Signature) {
			return fs.Name
		}
	}

	extFsType := detectExtFilesystem(file, offset)
	if extFsType != "Unknown" {
		return extFsType
	}

	return "Unknown"
}

// detectExtFilesystem detects ext2/ext3/ext4 filesystems by reading superblock
func detectExtFilesystem(file *os.File, offset int64) string {
	const superblockOffset = 0x400
	buffer := make([]byte, 0x70)

	_, err := file.ReadAt(buffer, offset+superblockOffset)
	if err != nil {
		return "Unknown"
	}

	magic := binary.LittleEndian.Uint16(buffer[0x38:0x3a])
	compatibleFeatures := binary.LittleEndian.Uint32(buffer[0x5c:0x60])

	if magic != 0xEF53 {
		return "Unknown"
	}

	if (compatibleFeatures & 0x40) == 0x40 {
		return "ext4"
	} else if (compatibleFeatures & 0x4) == 0x4 {
		return "ext3"
	}

	return "ext2"
}

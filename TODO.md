# Partition Management Feature TODO

This document tracks the implementation of partition create/delete/modify functionality for both GPT and MBR partition tables.

## Status Legend
- ⬜ Pending
- 🔄 In Progress
- ✅ Completed
- ❌ Cancelled

---

## 0. TUI Interface (Priority Task)

### ✅ Task 0: Implement TUI disk selector using planetui
- Add planetui package dependency
- Create `tui` command that displays interactive disk selector
- Show all available disks in a navigable list
- Arrow keys for navigation
- Right arrow or Enter to select disk and show partitions
- Q key and Ctrl+C to exit
- Display partition information for selected disk

---

## 1. Command Structure

### ⬜ Task 1: Add partition management command structure in main.go
- Add partition management command structure in main.go (partition create/delete/modify subcommands)
- Create cobra command hierarchy: `dsktool partition create/delete/modify`
- Add command aliases and help text

---

## 2. Core Infrastructure

### ⬜ Task 2: Create partition_common.go with shared utilities
- Backup partition table functionality
- Detect partition table type (GPT/MBR)
- Validation helpers for partition operations
- Common error handling utilities

### ⬜ Task 3: Implement GPT CRC32 calculation functions
- Header CRC calculation
- Partition array CRC calculation
- CRC validation helpers
- Reference: GPT specification CRC32 algorithm

### ⬜ Task 4: Implement GUID generation utility for GPT partitions
- Generate TypeGUID for common partition types
- Generate UniqueGUID for new partitions
- GUID parsing and formatting utilities
- Support for standard partition type GUIDs (Linux, Windows, EFI, etc.)

---

## 3. GPT Operations

### ⬜ Task 5: Create gpt_operations.go with core functions
- `readGPTHeader()` - Read and parse GPT header
- `readGPTPartitions()` - Read partition entry array
- `writeGPTHeader()` - Write GPT header with proper CRC
- `writeGPTPartitions()` - Write partition entry array with CRC
- `updateGPTCRC()` - Recalculate and update CRCs
- Handle both primary and backup GPT headers

### ⬜ Task 6: Implement GPT partition create
- Validate free space availability
- Find available partition entry slot
- Create partition entry with proper GUIDs
- Calculate and update partition array CRC
- Update backup GPT header
- Validate partition boundaries (FirstUsableLBA, LastUsableLBA)
- Support partition name (UTF-16LE encoding)

### ⬜ Task 7: Implement GPT partition delete
- Find partition by index or GUID
- Zero out partition entry
- Update partition array CRC
- Update backup GPT header
- Validate partition is not in use (mounted)

### ⬜ Task 8: Implement GPT partition modify
- Update partition size/position
- Change TypeGUID
- Update partition name
- Validate no overlaps with other partitions
- Update CRCs for both primary and backup headers
- Support modifying: FirstLBA, LastLBA, TypeGUID, PartitionName, AttributeFlags

---

## 4. MBR Operations

### ⬜ Task 9: Create mbr_operations.go with core functions
- `readMBR()` - Read MBR structure
- `writeMBR()` - Write MBR with signature
- `validateMBRSignature()` - Verify 0xAA55 signature
- `findFreeMBRSlot()` - Find available primary partition slot
- Handle extended partition chain reading/writing

### ⬜ Task 10: Implement MBR partition create
- Validate 4 primary partition limit
- Find free primary partition slot
- Calculate CHS (Cylinder-Head-Sector) values
- Write partition entry with proper status byte
- Handle extended partition creation if needed
- Support logical partition creation in extended partitions

### ⬜ Task 11: Implement MBR partition delete
- Find partition by index
- Zero out partition entry
- Handle extended partition cleanup if deleting extended partition
- Clean up logical partitions in extended partition chain if needed
- Validate partition is not in use

### ⬜ Task 12: Implement MBR partition modify
- Update partition type byte
- Update size and start sector
- Validate CHS values
- Handle extended partition updates
- Support modifying: Status, Type, FirstSector, Sectors

---

## 5. Safety & Validation

### ⬜ Task 13: Add safety features
- Confirmation prompts for destructive operations
- Automatic backup before modifications (save to file)
- Rollback capability (restore from backup)
- Dry-run mode to preview changes
- Warning messages for risky operations

### ⬜ Task 14: Add validation
- Check for mounted partitions (prevent modification of mounted partitions)
- Verify partition overlaps (no overlapping partitions)
- Validate sector alignment (4K alignment for modern disks)
- Check disk size limits
- Validate partition boundaries
- Check partition table integrity before operations

---

## 6. Platform Support

### ⬜ Task 15: Implement platform-specific write permissions check
- Linux: Check O_RDWR access, verify device is not mounted
- Windows: Check GENERIC_WRITE access, verify disk is not in use
- Darwin: Check write access, verify device permissions
- Provide clear error messages for permission issues

---

## 7. CLI Interface

### ⬜ Task 16: Add partition create command flags
- `--type` - GPT TypeGUID or MBR partition type (hex or name)
- `--size` - Partition size (with units: MB, GB, or sectors)
- `--start` - Starting sector/LBA (optional, auto-align if not specified)
- `--name` - Partition name (GPT only, UTF-16LE encoded)
- `--align` - Alignment in sectors (default: 2048 for 1MB alignment)
- `--guid` - Custom TypeGUID for GPT (optional)
- Support interactive mode for guided partition creation

### ⬜ Task 17: Add partition delete command
- `--index` - Partition index (0-based for MBR, entry index for GPT)
- `--guid` - Partition GUID for GPT (alternative to index)
- `--force` - Skip confirmation prompt
- `--backup` - Save backup before deletion
- Display partition info before deletion for confirmation

### ⬜ Task 18: Add partition modify command
- `--index` - Partition index to modify
- `--guid` - Partition GUID for GPT (alternative to index)
- `--size` - New partition size
- `--start` - New starting sector/LBA
- `--type` - New partition type (TypeGUID for GPT, type byte for MBR)
- `--name` - New partition name (GPT only)
- `--grow` - Grow partition to fill available space
- `--shrink` - Shrink partition (with validation)
- Validate all changes before applying

---

## 8. Error Handling & Testing

### ⬜ Task 19: Add error handling
- Proper error messages with context
- Transaction-like behavior (all-or-nothing operations)
- Detailed validation errors
- Recovery from partial failures
- Clear error messages for common issues (permissions, mounted, invalid parameters)

### ⬜ Task 20: Test partition operations on all platforms
- Linux: Test with loop devices and real disks
- Windows: Test with virtual disks and physical drives
- Darwin: Test with disk images and physical devices
- Test GPT operations (create, delete, modify)
- Test MBR operations (create, delete, modify)
- Test error cases (permissions, mounted partitions, invalid operations)
- Test edge cases (full partition table, overlapping partitions, etc.)

---

## Implementation Notes

### GPT Considerations
- Must maintain both primary and backup GPT headers
- CRCs must be recalculated after any modification
- Partition entries are typically 128 bytes
- Partition names are UTF-16LE encoded, max 36 characters
- FirstUsableLBA and LastUsableLBA define valid partition boundaries

### MBR Considerations
- Limited to 4 primary partitions (or 3 primary + 1 extended)
- Extended partitions can contain logical partitions via EBR chain
- CHS addressing is legacy but may be required for compatibility
- MBR signature (0xAA55) must be preserved
- Partition status byte: 0x80 = active, 0x00 = inactive

### Safety First
- Always backup partition tables before modifications
- Verify operations on test disks first
- Provide clear warnings for destructive operations
- Check for mounted partitions before modification
- Validate all inputs before writing to disk

---

## Dependencies

- Existing partition reading code (main_linux.go, main_darwin.go, main_windows.go)
- GPT header and partition structures (structs.go, structs_linux.go)
- MBR structures (structs.go, mbr_extended.go)
- CRC32 calculation (need to implement or use existing library)
- GUID generation (need to implement or use existing library)

---

## References

- GPT Specification (UEFI Specification, Part 2)
- MBR Partition Table Format
- Platform-specific disk I/O requirements
- Sector alignment best practices (4K alignment)

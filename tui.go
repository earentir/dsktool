package main

import (
	"fmt"
	"os"
	"strings"

	tcell "github.com/gdamore/tcell/v2"
)

// getDiskList returns a list of available disks for TUI
func getDiskList() []DiskInfo {
	return getDiskListData()
}

// getPartitionInfo returns partition information as a string for a given disk
func getPartitionInfo(diskPath string) string {
	return capturePartitionOutput(diskPath)
}

// tuiState holds the TUI state
type tuiState struct {
	disks                []DiskInfo
	selectedIndex        int
	showingPartitions    bool
	currentDisk          string
	partitionInfo        string
	partitions           []PartitionInfo
	selectedPartitionIdx int
}

// runTUI is the main entry point for the TUI command
func runTUI() {
	// Get disk list first - this should be fast
	disks := getDiskList()
	if len(disks) == 0 {
		fmt.Fprintf(os.Stderr, "No disks found\n")
		os.Exit(1)
	}

	state := &tuiState{
		disks:                disks,
		selectedIndex:        0,
		showingPartitions:    false,
		selectedPartitionIdx: 0,
	}

	// Run the interactive TUI
	state.runInteractiveTUI()
}

func (s *tuiState) runInteractiveTUI() {
	screen, err := tcell.NewScreen()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create screen: %v\n", err)
		os.Exit(1)
	}
	if err := screen.Init(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize screen: %v\n", err)
		os.Exit(1)
	}
	defer screen.Fini()

	screen.SetStyle(tcell.StyleDefault.
		Foreground(tcell.ColorWhite).
		Background(tcell.ColorBlack))
	screen.Clear()
	screen.Show()

	for {
		if !s.showingPartitions {
			s.renderDiskListTUI(screen)
		} else {
			s.renderPartitionsTUI(screen)
		}
		screen.Show()

		ev := screen.PollEvent()
		switch ev := ev.(type) {
		case *tcell.EventKey:
			if ev.Key() == tcell.KeyEscape || ev.Key() == tcell.KeyCtrlC {
				return
			}
			if ev.Rune() == 'q' || ev.Rune() == 'Q' {
				return
			}
			if s.handleKeyEvent(ev, screen) {
				return
			}
		case *tcell.EventResize:
			screen.Sync()
		}
	}
}

func (s *tuiState) renderDiskListTUI(screen tcell.Screen) {
	screen.Clear()
	width, height := screen.Size()

	// Title
	title := "=== Available Disks ==="
	titleX := (width - len(title)) / 2
	if titleX < 0 {
		titleX = 0
	}
	for i, ch := range title {
		if titleX+i >= width {
			break
		}
		screen.SetContent(titleX+i, 0, ch, nil, tcell.StyleDefault.Bold(true))
	}

	// Ensure selected index is valid
	if s.selectedIndex < 0 {
		s.selectedIndex = 0
	}
	if s.selectedIndex >= len(s.disks) && len(s.disks) > 0 {
		s.selectedIndex = len(s.disks) - 1
	}

	// Render disk list - just show disk path
	y := 2
	for i, disk := range s.disks {
		if y >= height-3 {
			break
		}

		var style tcell.Style
		var prefix string
		if i == s.selectedIndex {
			style = tcell.StyleDefault.
				Foreground(tcell.ColorBlack).
				Background(tcell.ColorWhite)
			prefix = "> "
		} else {
			style = tcell.StyleDefault
			prefix = "  "
		}

		// Show disk path with type indicator
		diskTypeLabel := ""
		switch disk.DiskType {
		case "synthesized":
			diskTypeLabel = " [synthesized]"
		case "image":
			diskTypeLabel = " [image]"
		case "physical":
			diskTypeLabel = " [physical]"
		}
		line := prefix + disk.Path + diskTypeLabel
		if len(line) > width {
			line = line[:width-1]
		}
		for x, ch := range line {
			if x >= width {
				break
			}
			screen.SetContent(x, y, ch, nil, style)
		}
		y++
	}

	// Status line at bottom - show selected disk info
	statusY := height - 2

	// Clear the status line area
	for x := 0; x < width; x++ {
		screen.SetContent(x, statusY, ' ', nil, tcell.StyleDefault.Reverse(true))
	}

	if len(s.disks) > 0 && s.selectedIndex >= 0 && s.selectedIndex < len(s.disks) {
		selectedDisk := s.disks[s.selectedIndex]

		var leftText string  // Mount point (left aligned)
		var rightText string // Disk info (right aligned)

		// Add disk type to left text if available
		typePrefix := ""
		if selectedDisk.DiskType != "" && selectedDisk.DiskType != "unknown" {
			typePrefix = fmt.Sprintf("[%s] ", selectedDisk.DiskType)
		}

		if selectedDisk.Mounted {
			// Extract mount point
			if idx := strings.Index(selectedDisk.MountInfo, "(mounted on "); idx != -1 {
				start := idx + len("(mounted on ")
				if end := strings.Index(selectedDisk.MountInfo[start:], ")"); end != -1 {
					leftText = typePrefix + "Mounted on: " + selectedDisk.MountInfo[start:start+end]
				}
			}
			if leftText == "" {
				leftText = typePrefix + "Mounted"
			}

			// Extract filesystem info (Total, Used, Free)
			if idx := strings.Index(selectedDisk.MountInfo, " - Total: "); idx != -1 {
				// Extract everything after " - Total: "
				fsInfo := selectedDisk.MountInfo[idx+3:] // Skip " - "
				rightText = fsInfo
			} else if strings.Contains(selectedDisk.MountInfo, "Error reading filesystem") {
				rightText = "Error reading filesystem"
			}
		} else {
			leftText = typePrefix + "Not mounted"
			if selectedDisk.Size > 0 {
				rightText = "Size: " + selectedDisk.SizeStr
			} else {
				rightText = selectedDisk.SizeStr
			}
		}

		// Write left text (mount point)
		x := 0
		for _, ch := range leftText {
			if x >= width {
				break
			}
			screen.SetContent(x, statusY, ch, nil, tcell.StyleDefault.Reverse(true))
			x++
		}

		// Write right text (disk info) - right aligned
		if len(rightText) > 0 {
			rightX := width - len(rightText)
			if rightX < 0 {
				rightX = 0
				rightText = rightText[:width]
			}
			// Make sure there's at least one space between left and right
			if rightX <= x {
				rightX = x + 1
				if rightX+len(rightText) > width {
					rightText = rightText[:width-rightX]
				}
			}
			for i, ch := range rightText {
				if rightX+i >= width {
					break
				}
				screen.SetContent(rightX+i, statusY, ch, nil, tcell.StyleDefault.Reverse(true))
			}
		}
	}

	// Instructions
	instructions := "↑↓: Navigate | →/Enter: Select | Q/Ctrl+C: Quit"
	instY := height - 1
	instX := (width - len(instructions)) / 2
	if instX < 0 {
		instX = 0
	}
	for i, ch := range instructions {
		if instX+i >= width {
			break
		}
		screen.SetContent(instX+i, instY, ch, nil, tcell.StyleDefault.Dim(true))
	}
}

func (s *tuiState) renderPartitionsTUI(screen tcell.Screen) {
	screen.Clear()
	width, height := screen.Size()

	// Title
	title := fmt.Sprintf("=== Partitions for %s ===", s.currentDisk)
	titleX := (width - len(title)) / 2
	if titleX < 0 {
		titleX = 0
	}
	for i, ch := range title {
		if titleX+i >= width {
			break
		}
		screen.SetContent(titleX+i, 0, ch, nil, tcell.StyleDefault.Bold(true))
	}

	// Ensure selected partition index is valid
	if s.selectedPartitionIdx < 0 {
		s.selectedPartitionIdx = 0
	}
	if s.selectedPartitionIdx >= len(s.partitions) && len(s.partitions) > 0 {
		s.selectedPartitionIdx = len(s.partitions) - 1
	}

	// fdisk-style header
	headerY := 2
	headerLine := "         Starting       Ending"
	headerX := 0
	for i, ch := range headerLine {
		if headerX+i >= width {
			break
		}
		screen.SetContent(headerX+i, headerY, ch, nil, tcell.StyleDefault.Bold(true))
	}

	headerLine2 := " #: id  cyl  hd sec -  cyl  hd sec [     start -       size]"
	headerY2 := headerY + 1
	headerX2 := 0
	for i, ch := range headerLine2 {
		if headerX2+i >= width {
			break
		}
		screen.SetContent(headerX2+i, headerY2, ch, nil, tcell.StyleDefault.Bold(true))
	}

	// Separator line
	sepY := headerY2 + 1
	sepLine := strings.Repeat("-", width)
	for i, ch := range sepLine {
		if i >= width {
			break
		}
		screen.SetContent(i, sepY, ch, nil, tcell.StyleDefault)
	}

	// Draw partition rows in fdisk style
	y := sepY + 1
	if len(s.partitions) == 0 {
		// Show error message if no partitions
		msg := "No partitions found or error reading partitions"
		msgX := (width - len(msg)) / 2
		if msgX < 0 {
			msgX = 0
		}
		for i, ch := range msg {
			if msgX+i >= width {
				break
			}
			screen.SetContent(msgX+i, y, ch, nil, tcell.StyleDefault.Dim(true))
		}
	} else {
		for i, part := range s.partitions {
			if y >= height-4 {
				break
			}

			var rowStyle tcell.Style
			if i == s.selectedPartitionIdx {
				rowStyle = tcell.StyleDefault.
					Foreground(tcell.ColorBlack).
					Background(tcell.ColorWhite)
			} else if part.Unused {
				// Unused space - dimmed style but still selectable
				rowStyle = tcell.StyleDefault.Dim(true)
			} else {
				rowStyle = tcell.StyleDefault
			}

			// Format partition in fdisk style: *1: 0C    0  32  33 -  940 254  63 [      2048 -   15124480] Win95 FAT32L
			activeMark := " "
			if part.Status == "Active" || strings.HasPrefix(part.Status, "Active") {
				activeMark = "*"
			}

			partTypeID := part.Type
			// Extract hex ID if it's in format "0x02" or just use first part
			if strings.HasPrefix(partTypeID, "0x") {
				partTypeID = strings.ToUpper(partTypeID[2:])
				if len(partTypeID) > 2 {
					partTypeID = partTypeID[:2]
				}
			} else if len(partTypeID) > 2 {
				// For GPT GUIDs, just show first 2 chars or "  "
				partTypeID = "  "
			}
			// Ensure it's 2 chars, uppercase
			if len(partTypeID) < 2 {
				partTypeID = strings.ToUpper(partTypeID) + strings.Repeat(" ", 2-len(partTypeID))
			} else {
				partTypeID = strings.ToUpper(partTypeID[:2])
			}

			firstLBA := part.FirstLBA
			lastLBAVal := part.LastLBA
			if lastLBAVal == 0 && part.FirstLBA > 0 && part.TotalSectors > 0 {
				lastLBAVal = part.FirstLBA + part.TotalSectors - 1
			}

			// Format: *1: 0C    0  32  33 -  940 254  63 [      2048 -   15124480] Win95 FAT32L
			// We don't have CHS values, so show zeros or calculate approximate values
			// For now, show zeros for CHS
			startCyl, startHd, startSec := uint32(0), uint32(0), uint32(0)
			endCyl, endHd, endSec := uint32(0), uint32(0), uint32(0)

			// Try to calculate approximate CHS if we have sector size
			if part.SectorSize > 0 {
				// Rough approximation: assume 63 sectors per track, 255 heads
				sectorsPerTrack := uint32(63)
				heads := uint32(255)
				startCyl = uint32(firstLBA) / (sectorsPerTrack * heads)
				startHd = (uint32(firstLBA) / sectorsPerTrack) % heads
				startSec = (uint32(firstLBA) % sectorsPerTrack) + 1 // CHS sectors are 1-based

				endCyl = uint32(lastLBAVal) / (sectorsPerTrack * heads)
				endHd = (uint32(lastLBAVal) / sectorsPerTrack) % heads
				endSec = (uint32(lastLBAVal) % sectorsPerTrack) + 1
			}

			displayName := part.FileSystem
			if part.Unused {
				displayName = "Unused"
			} else if displayName == "" {
				displayName = "Unknown"
			}
			if part.Name != "" && part.Name != fmt.Sprintf("Partition %d", part.Number) && !part.Unused {
				displayName = part.Name
			}

			// Build the line with proper column alignment matching fdisk exactly
			// Format: *1: 0C    0  32  33 -  940 254  63 [      2048 -   15124480] Win95 FAT32L
			// The format is: [*]NN: II CCCC HHH SSS - CCCC HHH SSS [ SSSSSSSSSS - SSSSSSSSSS] Name
			line := fmt.Sprintf("%s%d: %2s %4d %3d %3d - %4d %3d %3d [%10d - %10d] %s",
				activeMark, part.Number, partTypeID,
				startCyl, startHd, startSec,
				endCyl, endHd, endSec,
				firstLBA, part.TotalSectors, displayName)

			// Draw the line character by character to handle selection highlighting
			x := 0
			for _, ch := range line {
				if x >= width {
					break
				}
				screen.SetContent(x, y, ch, nil, rowStyle)
				x++
			}
			y++
		}
	}

	// Status line at bottom - show selected partition details
	statusY := height - 2

	// Clear the status line area
	for x := 0; x < width; x++ {
		screen.SetContent(x, statusY, ' ', nil, tcell.StyleDefault.Reverse(true))
	}

	if len(s.partitions) > 0 && s.selectedPartitionIdx >= 0 && s.selectedPartitionIdx < len(s.partitions) {
		selectedPart := s.partitions[s.selectedPartitionIdx]

		var leftText string  // Mount point (left aligned)
		var rightText string // Partition info (right aligned)

		if selectedPart.Unused {
			// Special handling for unused space
			leftText = "Unpartitioned space"
			var details []string
			if selectedPart.Size != "" {
				details = append(details, "Size: "+selectedPart.Size)
			}
			if selectedPart.SectorSize > 0 {
				details = append(details, fmt.Sprintf("Sector: %d bytes", selectedPart.SectorSize))
			}
			if selectedPart.FirstLBA > 0 {
				details = append(details, fmt.Sprintf("FirstLBA: %d", selectedPart.FirstLBA))
			}
			if selectedPart.LastLBA > 0 {
				details = append(details, fmt.Sprintf("LastLBA: %d", selectedPart.LastLBA))
			}
			rightText = strings.Join(details, " | ")
		} else {
			// Build left text with mount point
			if selectedPart.Mounted && selectedPart.MountPoint != "" {
				leftText = fmt.Sprintf("Mounted on: %s", selectedPart.MountPoint)
			} else {
				leftText = "Not mounted"
			}

			// Build right text with partition details
			var details []string
			if selectedPart.Size != "" {
				details = append(details, "Size: "+selectedPart.Size)
			}
			if selectedPart.SectorSize > 0 {
				details = append(details, fmt.Sprintf("Sector: %d bytes", selectedPart.SectorSize))
			}
			if selectedPart.TypeGUID != "" && selectedPart.TypeGUID != "-" {
				details = append(details, "TypeGUID: "+selectedPart.TypeGUID)
			}
			if selectedPart.UniqueGUID != "" && selectedPart.UniqueGUID != "-" {
				details = append(details, "UniqueGUID: "+selectedPart.UniqueGUID)
			}
			if selectedPart.FirstLBA > 0 {
				details = append(details, fmt.Sprintf("FirstLBA: %d", selectedPart.FirstLBA))
			}
			if selectedPart.LastLBA > 0 {
				details = append(details, fmt.Sprintf("LastLBA: %d", selectedPart.LastLBA))
			}

			// Add filesystem info if mounted
			if selectedPart.Mounted && selectedPart.MountInfo != "" {
				if idx := strings.Index(selectedPart.MountInfo, " - Total: "); idx != -1 {
					fsInfo := selectedPart.MountInfo[idx+3:] // Skip " - "
					rightText = fsInfo
				} else {
					rightText = strings.Join(details, " | ")
				}
			} else {
				rightText = strings.Join(details, " | ")
			}
		}

		// Write left text (mount point)
		x := 0
		for _, ch := range leftText {
			if x >= width {
				break
			}
			screen.SetContent(x, statusY, ch, nil, tcell.StyleDefault.Reverse(true))
			x++
		}

		// Write right text (partition info) - right aligned
		if len(rightText) > 0 {
			rightX := width - len(rightText)
			if rightX < 0 {
				rightX = 0
				rightText = rightText[:width]
			}
			// Make sure there's at least one space between left and right
			if rightX <= x {
				rightX = x + 1
				if rightX+len(rightText) > width {
					rightText = rightText[:width-rightX]
				}
			}
			for i, ch := range rightText {
				if rightX+i >= width {
					break
				}
				screen.SetContent(rightX+i, statusY, ch, nil, tcell.StyleDefault.Reverse(true))
			}
		}
	}

	// Instructions
	instructions := "↑↓: Navigate | ←/B: Back | Q/Ctrl+C: Quit"
	instY := height - 1
	instX := (width - len(instructions)) / 2
	if instX < 0 {
		instX = 0
	}
	for i, ch := range instructions {
		if instX+i >= width {
			break
		}
		screen.SetContent(instX+i, instY, ch, nil, tcell.StyleDefault.Dim(true))
	}
}

func (s *tuiState) handleKeyEvent(ev *tcell.EventKey, _ tcell.Screen) bool {
	if s.showingPartitions {
		// In partition view
		switch ev.Key() {
		case tcell.KeyLeft:
			// Left arrow goes back
			s.showingPartitions = false
			s.currentDisk = ""
			s.partitionInfo = ""
			s.partitions = nil
			s.selectedPartitionIdx = 0
			return false
		case tcell.KeyUp:
			if s.selectedPartitionIdx > 0 {
				s.selectedPartitionIdx--
			}
			return false
		case tcell.KeyDown:
			if s.selectedPartitionIdx < len(s.partitions)-1 {
				s.selectedPartitionIdx++
			}
			return false
		case tcell.KeyRight, tcell.KeyEnter:
			// Future: show partition options
			// For now, just return false to stay in partition view
			return false
		}
		// Check for 'b' or 'B' to go back
		if ev.Rune() == 'b' || ev.Rune() == 'B' {
			s.showingPartitions = false
			s.currentDisk = ""
			s.partitionInfo = ""
			s.partitions = nil
			s.selectedPartitionIdx = 0
			return false
		}
		return false
	}

	// In disk list view
	switch ev.Key() {
	case tcell.KeyUp:
		if s.selectedIndex > 0 {
			s.selectedIndex--
		}
		return false
	case tcell.KeyDown:
		if s.selectedIndex < len(s.disks)-1 {
			s.selectedIndex++
		}
		return false
	case tcell.KeyRight, tcell.KeyEnter:
		if len(s.disks) > 0 && s.selectedIndex >= 0 && s.selectedIndex < len(s.disks) {
			selectedDisk := s.disks[s.selectedIndex]
			s.currentDisk = selectedDisk.Path
			s.partitionInfo = getPartitionInfo(selectedDisk.Path)

			// Parse partitions into structured data
			partitions, err := getPartitionsData(selectedDisk.Path)
			if err != nil {
				// If parsing fails, still show raw output
				s.partitions = nil
			} else {
				s.partitions = partitions
				s.selectedPartitionIdx = 0
			}

			s.showingPartitions = true
		}
		return false
	}

	return false
}

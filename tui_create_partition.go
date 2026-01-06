package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/gdamore/tcell/v2"
)

// checkIfGPTDisk checks if a disk uses GPT partition table
func checkIfGPTDisk(file *os.File) bool {
	_, err := file.Seek(512, io.SeekStart)
	if err != nil {
		return false
	}

	header := gptHeader{}
	err = binary.Read(file, binary.LittleEndian, &header)
	if err != nil {
		return false
	}

	return string(header.Signature[:]) == "EFI PART"
}

// PartitionCreateForm represents the form state for creating a partition
type PartitionCreateForm struct {
	fields            []FormField
	selectedField     int
	diskPath          string
	unusedPartition   PartitionInfo
	isGPT             bool // true for GPT, false for MBR
	diskSize          int64 // Total disk size in bytes
	availableMBRSlots []string // Available MBR partition numbers (1-4)
}

// FormField represents a single form field
type FormField struct {
	label    string
	value    string
	fieldType string // "text", "select"
	options  []string // for select fields
}

// renderCreatePartitionForm renders the partition creation form
func (s *tuiState) renderCreatePartitionForm(screen tcell.Screen, width, height int) {
	form := s.createPartitionForm

	// Calculate form size
	formWidth := 70
	formHeight := len(form.fields) + 8 // fields + title + separator + buttons + borders
	formX := (width - formWidth) / 2
	formY := (height - formHeight) / 2

	// Ensure form fits on screen
	if formX < 0 {
		formX = 0
	}
	if formY < 0 {
		formY = 0
	}
	if formX+formWidth > width {
		formWidth = width - formX
	}
	if formY+formHeight > height {
		formHeight = height - formY
	}

	// Draw semi-transparent overlay
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			screen.SetContent(x, y, ' ', nil, tcell.StyleDefault.Dim(true))
		}
	}

	// Draw form border
	for y := formY; y < formY+formHeight; y++ {
		for x := formX; x < formX+formWidth; x++ {
			if y == formY {
				// Top border
				if x == formX {
					screen.SetContent(x, y, '┌', nil, tcell.StyleDefault.Bold(true))
				} else if x == formX+formWidth-1 {
					screen.SetContent(x, y, '┐', nil, tcell.StyleDefault.Bold(true))
				} else {
					screen.SetContent(x, y, '─', nil, tcell.StyleDefault.Bold(true))
				}
			} else if y == formY+formHeight-1 {
				// Bottom border
				if x == formX {
					screen.SetContent(x, y, '└', nil, tcell.StyleDefault.Bold(true))
				} else if x == formX+formWidth-1 {
					screen.SetContent(x, y, '┘', nil, tcell.StyleDefault.Bold(true))
				} else {
					screen.SetContent(x, y, '─', nil, tcell.StyleDefault.Bold(true))
				}
			} else if x == formX || x == formX+formWidth-1 {
				// Side borders
				screen.SetContent(x, y, '│', nil, tcell.StyleDefault.Bold(true))
			} else {
				// Background
				screen.SetContent(x, y, ' ', nil, tcell.StyleDefault.Reverse(true))
			}
		}
	}

	// Draw title
	title := "Create Partition"
	titleX := formX + (formWidth-len(title))/2
	for i, ch := range title {
		if titleX+i < formX+formWidth-1 {
			screen.SetContent(titleX+i, formY+1, ch, nil, tcell.StyleDefault.Bold(true).Reverse(true))
		}
	}

	// Draw separator
	sepY := formY + 2
	screen.SetContent(formX, sepY, '├', nil, tcell.StyleDefault.Bold(true))
	for x := formX + 1; x < formX+formWidth-1; x++ {
		screen.SetContent(x, sepY, '─', nil, tcell.StyleDefault.Bold(true))
	}
	screen.SetContent(formX+formWidth-1, sepY, '┤', nil, tcell.StyleDefault.Bold(true))

	// Draw fields
	fieldY := formY + 3
	for i, field := range form.fields {
		if fieldY+i >= formY+formHeight-3 {
			break
		}

		// Draw label
		labelX := formX + 2
		labelStyle := tcell.StyleDefault.Reverse(true)
		if i == form.selectedField {
			labelStyle = labelStyle.Bold(true)
		}
		for j, ch := range field.label {
			if labelX+j < formX+formWidth-2 {
				screen.SetContent(labelX+j, fieldY+i, ch, nil, labelStyle)
			}
		}

		// Draw field value
		valueX := formX + 20
		valueStyle := tcell.StyleDefault.Reverse(true)
		if i == form.selectedField {
			valueStyle = valueStyle.Foreground(tcell.ColorBlack).Background(tcell.ColorWhite)
		}

		// Draw selection marker
		if i == form.selectedField {
			screen.SetContent(valueX-1, fieldY+i, '▶', nil, valueStyle)
		} else {
			screen.SetContent(valueX-1, fieldY+i, ' ', nil, valueStyle)
		}

		// Draw value
		displayValue := field.value
		if displayValue == "" {
			displayValue = "(empty)"
		}
		maxValueWidth := formX + formWidth - valueX - 2
		if len(displayValue) > maxValueWidth {
			displayValue = displayValue[:maxValueWidth]
		}
		for j, ch := range displayValue {
			if valueX+j < formX+formWidth-2 {
				screen.SetContent(valueX+j, fieldY+i, ch, nil, valueStyle)
			}
		}
		// Fill remaining space
		for j := len(displayValue); j < maxValueWidth; j++ {
			if valueX+j < formX+formWidth-2 {
				screen.SetContent(valueX+j, fieldY+i, ' ', nil, valueStyle)
			}
		}
	}

	// Draw instructions
	instructions := "Tab: Next  Shift+Tab: Previous  Enter: Save  Esc: Cancel"
	instX := formX + (formWidth-len(instructions))/2
	instY := formY + formHeight - 2
	for i, ch := range instructions {
		if instX+i < formX+formWidth-1 {
			screen.SetContent(instX+i, instY, ch, nil, tcell.StyleDefault.Dim(true).Reverse(true))
		}
	}
}

// initCreatePartitionForm initializes the create partition form
func (s *tuiState) initCreatePartitionForm(unusedPart PartitionInfo) {
	// Get disk size for validation
	var diskSize int64
	if size, err := getBlockDeviceSizePlatform(s.currentDisk); err == nil {
		diskSize = size
	}

	// Find available MBR partition slots (1-4)
	usedSlots := make(map[int]bool)
	for _, p := range s.partitions {
		if !p.Unused && p.Number >= 1 && p.Number <= 4 {
			usedSlots[p.Number] = true
		}
	}
	availableMBRSlots := []string{}
	for i := 1; i <= 4; i++ {
		if !usedSlots[i] {
			availableMBRSlots = append(availableMBRSlots, fmt.Sprintf("%d", i))
		}
	}
	if len(availableMBRSlots) == 0 {
		availableMBRSlots = []string{"1", "2", "3", "4"} // All slots, user will get error if all used
	}

	// Determine GPT partition number (next available)
	nextGPTNum := 1
	if len(s.partitions) > 0 {
		maxNum := 0
		for _, p := range s.partitions {
			if !p.Unused && p.Number > maxNum {
				maxNum = p.Number
			}
		}
		nextGPTNum = maxNum + 1
	}

	fields := []FormField{
		{label: "Partition Scheme:", value: "GPT", fieldType: "select", options: []string{"GPT", "MBR"}},
		{label: "Partition Number:", value: fmt.Sprintf("%d", nextGPTNum), fieldType: "text"}, // Will be updated based on scheme
		{label: "Partition Name:", value: "", fieldType: "text"}, // GPT only
		{label: "Start LBA:", value: fmt.Sprintf("%d", unusedPart.FirstLBA), fieldType: "text"},
		{label: "Size or End LBA:", value: "", fieldType: "text"}, // Accepts "1G", "500M", "1000000" (end LBA)
		{label: "Type:", value: "Linux Filesystem", fieldType: "select", options: []string{
			"Linux Filesystem",
			"Linux Swap",
			"EFI System",
			"Windows Basic Data",
			"Microsoft Reserved",
			"Extended Partition", // MBR only
		}},
	}

	s.createPartitionForm = PartitionCreateForm{
		fields:          fields,
		selectedField:   0,
		diskPath:        s.currentDisk,
		unusedPartition: unusedPart,
		isGPT:           true, // Default, will be updated based on scheme selection
		diskSize:        diskSize,
		availableMBRSlots: availableMBRSlots,
	}
}

// handleCreatePartitionFormKey handles keyboard input for the create partition form
func (s *tuiState) handleCreatePartitionFormKey(ev *tcell.EventKey) bool {
	form := &s.createPartitionForm

	switch ev.Key() {
	case tcell.KeyTab:
		// Move to next field
		if ev.Modifiers()&tcell.ModShift != 0 {
			// Shift+Tab: previous field
			if form.selectedField > 0 {
				form.selectedField--
			} else {
				form.selectedField = len(form.fields) - 1
			}
		} else {
			// Tab: next field
			if form.selectedField < len(form.fields)-1 {
				form.selectedField++
			} else {
				form.selectedField = 0
			}
		}
		return false
	case tcell.KeyEnter:
		// Validate and show confirmation
		if s.validateCreatePartitionForm() {
			s.showCreatePartitionConfirm = true
			s.showCreatePartitionForm = false
		}
		return false
	case tcell.KeyEsc:
		// Cancel
		s.showCreatePartitionForm = false
		s.createPartitionForm = PartitionCreateForm{}
		return false
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		// Delete character
		field := &form.fields[form.selectedField]
		if len(field.value) > 0 {
			field.value = field.value[:len(field.value)-1]
		}
		return false
	case tcell.KeyLeft:
		// Move cursor left (for text fields)
		return false
	case tcell.KeyRight:
		// Move cursor right (for text fields)
		return false
	case tcell.KeyUp:
		// Previous field
		if form.selectedField > 0 {
			form.selectedField--
		}
		return false
	case tcell.KeyDown:
		// Next field
		if form.selectedField < len(form.fields)-1 {
			form.selectedField++
		}
		return false
	}

	// Handle text input
	if ev.Rune() >= 32 && ev.Rune() < 127 {
		field := &form.fields[form.selectedField]
		if field.fieldType == "text" {
			field.value += string(ev.Rune())
		} else if field.fieldType == "select" {
			// For select fields, cycle through options
			currentIdx := -1
			for i, opt := range field.options {
				if opt == field.value {
					currentIdx = i
					break
				}
			}
			if currentIdx >= 0 {
				nextIdx := (currentIdx + 1) % len(field.options)
				field.value = field.options[nextIdx]
			} else if len(field.options) > 0 {
				field.value = field.options[0]
			}
		}
		return false
	}

	return false
}

// validateCreatePartitionForm validates the form and returns true if valid
func (s *tuiState) validateCreatePartitionForm() bool {
	form := &s.createPartitionForm

	// Check that size or end LBA is provided
	var sizeOrEndFieldIdx int
	if form.isGPT {
		sizeOrEndFieldIdx = 3 // GPT: [Partition Number, Name, Start LBA, Size/End LBA, Type]
	} else {
		sizeOrEndFieldIdx = 2 // MBR: [Partition Number, Start LBA, Size/End LBA, Type]
	}

	if len(form.fields) <= sizeOrEndFieldIdx {
		s.showError = true
		s.errorMessage = "Form fields incomplete"
		return false
	}

	sizeOrEndField := form.fields[sizeOrEndFieldIdx]
	if sizeOrEndField.value == "" {
		s.showError = true
		s.errorMessage = "Size or End LBA is required"
		return false
	}

	// Parse and validate, calculate preview
	preview, err := parsePartitionForm(form)
	if err != nil {
		s.showError = true
		s.errorMessage = fmt.Sprintf("Invalid input: %v", err)
		return false
	}

	// Validate partition number for MBR
	if !form.isGPT {
		if preview.Number < 1 || preview.Number > 4 {
			s.showError = true
			s.errorMessage = "MBR partition number must be 1-4"
			return false
		}
	}

	// Validate partition fits in unused space
	if preview.FirstLBA < form.unusedPartition.FirstLBA {
		s.showError = true
		s.errorMessage = fmt.Sprintf("Start LBA %d is before unused space start %d", preview.FirstLBA, form.unusedPartition.FirstLBA)
		return false
	}
	if form.unusedPartition.LastLBA > 0 && preview.LastLBA > form.unusedPartition.LastLBA {
		s.showError = true
		s.errorMessage = fmt.Sprintf("End LBA %d exceeds unused space end %d", preview.LastLBA, form.unusedPartition.LastLBA)
		return false
	}

	s.createPartitionPreview = preview
	return true
}

// parsePartitionForm parses the form data and returns a preview PartitionInfo
func parsePartitionForm(form *PartitionCreateForm) (PartitionInfo, error) {
	var preview PartitionInfo

	// Fields: [Partition Scheme, Partition Number, Partition Name, Start LBA, Size/End LBA, Type]
	schemeStr := form.fields[0].value
	form.isGPT = (schemeStr == "GPT")

	var partNum int
	var startLBA uint64
	var sizeOrEndStr string
	var typeStr string
	var nameStr string

	if form.isGPT {
		// GPT: fields are [Scheme, Partition Number, Partition Name, Start LBA, Size/End LBA, Type]
		partNumStr := form.fields[1].value
		if partNumStr != "" {
			_, err := fmt.Sscanf(partNumStr, "%d", &partNum)
			if err != nil {
				return preview, fmt.Errorf("invalid partition number: %w", err)
			}
		}
		nameStr = form.fields[2].value
		startLBAStr := form.fields[3].value
		sizeOrEndStr = form.fields[4].value
		typeStr = form.fields[5].value

		if startLBAStr != "" {
			_, err := fmt.Sscanf(startLBAStr, "%d", &startLBA)
			if err != nil {
				return preview, fmt.Errorf("invalid start LBA (must be numeric): %w", err)
			}
		} else {
			startLBA = form.unusedPartition.FirstLBA
		}
	} else {
		// MBR: fields are [Scheme, Partition Number (1-4), Partition Name (N/A), Start LBA, Size/End LBA, Type]
		partNumStr := form.fields[1].value
		if partNumStr != "" {
			_, err := fmt.Sscanf(partNumStr, "%d", &partNum)
			if err != nil {
				return preview, fmt.Errorf("invalid partition number: %w", err)
			}
			if partNum < 1 || partNum > 4 {
				return preview, fmt.Errorf("MBR partition number must be 1-4")
			}
		}
		startLBAStr := form.fields[3].value
		sizeOrEndStr = form.fields[4].value
		typeStr = form.fields[5].value

		if startLBAStr != "" {
			_, err := fmt.Sscanf(startLBAStr, "%d", &startLBA)
			if err != nil {
				return preview, fmt.Errorf("invalid start LBA (must be numeric): %w", err)
			}
		} else {
			startLBA = form.unusedPartition.FirstLBA
		}
	}

	// Parse size or end LBA
	var lastLBA uint64
	var totalSectors uint64
	sectorSize := uint64(512)
	if form.unusedPartition.SectorSize > 0 {
		sectorSize = form.unusedPartition.SectorSize
	}

	if sizeOrEndStr != "" {
		// Try to parse as end LBA first (just a number, no units)
		var endLBA uint64
		if _, err := fmt.Sscanf(sizeOrEndStr, "%d", &endLBA); err == nil && !strings.HasSuffix(strings.ToUpper(sizeOrEndStr), "B") && !strings.HasSuffix(strings.ToUpper(sizeOrEndStr), "K") && !strings.HasSuffix(strings.ToUpper(sizeOrEndStr), "M") && !strings.HasSuffix(strings.ToUpper(sizeOrEndStr), "G") && !strings.HasSuffix(strings.ToUpper(sizeOrEndStr), "T") {
			// It's an end LBA (pure number, no unit suffix)
			lastLBA = endLBA
			if lastLBA < startLBA {
				return preview, fmt.Errorf("end LBA must be >= start LBA")
			}
			totalSectors = lastLBA - startLBA + 1
		} else {
			// Try to parse as size with units (1G, 500M, etc.)
			sizeBytes, err := parseSizeWithUnits(sizeOrEndStr)
			if err != nil {
				return preview, fmt.Errorf("invalid size or end LBA: %w", err)
			}
			// Size is bytes after start LBA
			totalSectors = sizeBytes / sectorSize
			if sizeBytes%sectorSize != 0 {
				totalSectors++ // Round up
			}
			lastLBA = startLBA + totalSectors - 1

			// Validate it fits on disk
			if form.diskSize > 0 {
				maxLBA := uint64(form.diskSize) / sectorSize
				if lastLBA >= maxLBA {
					return preview, fmt.Errorf("partition extends beyond disk (end LBA %d >= disk size %d sectors)", lastLBA, maxLBA)
				}
			}
		}
	} else {
		return preview, fmt.Errorf("size or end LBA is required")
	}

	preview = PartitionInfo{
		Number:       partNum,
		Name:         nameStr,
		Type:         typeStr,
		FileSystem:   "",
		Size:         formatBytes(totalSectors * sectorSize),
		FirstLBA:     startLBA,
		LastLBA:      lastLBA,
		TotalSectors: totalSectors,
		SectorSize:   sectorSize,
		Unused:       false,
	}

	return preview, nil
}

// parseSizeWithUnits parses size strings like "1G", "500M", "1GB", "512MB" into bytes
func parseSizeWithUnits(sizeStr string) (uint64, error) {
	sizeStr = strings.TrimSpace(sizeStr)
	if sizeStr == "" {
		return 0, fmt.Errorf("empty size")
	}

	// Remove spaces and parse number + unit
	var size float64
	var unit string
	n, err := fmt.Sscanf(sizeStr, "%f%s", &size, &unit)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("invalid size format: %s", sizeStr)
	}

	unit = strings.ToUpper(strings.TrimSpace(unit))
	var multiplier uint64
	switch unit {
	case "B", "":
		multiplier = 1
	case "KB", "K":
		multiplier = 1024
	case "MB", "M":
		multiplier = 1024 * 1024
	case "GB", "G":
		multiplier = 1024 * 1024 * 1024
	case "TB", "T":
		multiplier = 1024 * 1024 * 1024 * 1024
	default:
		return 0, fmt.Errorf("unknown unit: %s (use B, K/KB, M/MB, G/GB, T/TB)", unit)
	}

	return uint64(size * float64(multiplier)), nil
}

// renderCreatePartitionConfirm renders the confirmation dialog for partition creation
func (s *tuiState) renderCreatePartitionConfirm(screen tcell.Screen, width, height int) {
	preview := s.createPartitionPreview
	form := s.createPartitionForm

	// Build confirmation message with partition details
	var details []string
	details = append(details, fmt.Sprintf("Scheme: %s", form.fields[0].value))
	details = append(details, fmt.Sprintf("Partition Number: %d", preview.Number))
	if form.isGPT {
		details = append(details, fmt.Sprintf("Name: %s", preview.Name))
	}
	details = append(details, fmt.Sprintf("Type: %s", preview.Type))
	details = append(details, fmt.Sprintf("Start LBA: %d", preview.FirstLBA))
	details = append(details, fmt.Sprintf("End LBA: %d", preview.LastLBA))
	details = append(details, fmt.Sprintf("Size: %s", preview.Size))
	details = append(details, fmt.Sprintf("Total Sectors: %d", preview.TotalSectors))
	if !form.isGPT && preview.Type == "Extended Partition" {
		details = append(details, "Note: Extended partition will contain logical partitions")
	}

	msg := "Create partition with these details?\n\n" + strings.Join(details, "\n")
	lines := strings.Split(msg, "\n")

	dialogWidth := 60
	maxLineLen := 0
	for _, line := range lines {
		if len(line) > maxLineLen {
			maxLineLen = len(line)
		}
	}
	if maxLineLen+4 > dialogWidth {
		dialogWidth = maxLineLen + 4
	}

	dialogHeight := len(lines) + 7
	dialogX := (width - dialogWidth) / 2
	dialogY := (height - dialogHeight) / 2

	// Ensure dialog fits on screen
	if dialogX < 0 {
		dialogX = 0
	}
	if dialogY < 0 {
		dialogY = 0
	}
	if dialogX+dialogWidth > width {
		dialogWidth = width - dialogX
	}
	if dialogY+dialogHeight > height {
		dialogHeight = height - dialogY
	}

	// Draw semi-transparent overlay
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			screen.SetContent(x, y, ' ', nil, tcell.StyleDefault.Dim(true))
		}
	}

	// Draw dialog border
	for y := dialogY; y < dialogY+dialogHeight; y++ {
		for x := dialogX; x < dialogX+dialogWidth; x++ {
			if y == dialogY {
				if x == dialogX {
					screen.SetContent(x, y, '┌', nil, tcell.StyleDefault.Bold(true))
				} else if x == dialogX+dialogWidth-1 {
					screen.SetContent(x, y, '┐', nil, tcell.StyleDefault.Bold(true))
				} else {
					screen.SetContent(x, y, '─', nil, tcell.StyleDefault.Bold(true))
				}
			} else if y == dialogY+dialogHeight-1 {
				if x == dialogX {
					screen.SetContent(x, y, '└', nil, tcell.StyleDefault.Bold(true))
				} else if x == dialogX+dialogWidth-1 {
					screen.SetContent(x, y, '┘', nil, tcell.StyleDefault.Bold(true))
				} else {
					screen.SetContent(x, y, '─', nil, tcell.StyleDefault.Bold(true))
				}
			} else if x == dialogX || x == dialogX+dialogWidth-1 {
				screen.SetContent(x, y, '│', nil, tcell.StyleDefault.Bold(true))
			} else {
				screen.SetContent(x, y, ' ', nil, tcell.StyleDefault.Reverse(true))
			}
		}
	}

	// Draw message lines
	msgY := dialogY + 2
	for i, line := range lines {
		if msgY+i >= dialogY+dialogHeight-3 {
			break
		}
		lineX := dialogX + (dialogWidth-len(line))/2
		for j, ch := range line {
			if lineX+j < dialogX+dialogWidth-1 {
				screen.SetContent(lineX+j, msgY+i, ch, nil, tcell.StyleDefault.Reverse(true))
			}
		}
	}

	// Draw Yes/No options
	yesNoY := dialogY + dialogHeight - 4
	yesX := dialogX + dialogWidth/2 - 10
	noX := dialogX + dialogWidth/2 + 5

	yesStyle := tcell.StyleDefault.Reverse(true)
	if s.selectedOptionIdx == 0 {
		yesStyle = tcell.StyleDefault.Foreground(tcell.ColorBlack).Background(tcell.ColorWhite).Reverse(true)
	}
	screen.SetContent(yesX, yesNoY, '[', nil, yesStyle)
	if s.selectedOptionIdx == 0 {
		screen.SetContent(yesX+1, yesNoY, 'X', nil, yesStyle)
	} else {
		screen.SetContent(yesX+1, yesNoY, ' ', nil, yesStyle)
	}
	screen.SetContent(yesX+2, yesNoY, ']', nil, yesStyle)
	for i, ch := range " Yes" {
		screen.SetContent(yesX+3+i, yesNoY, ch, nil, yesStyle)
	}

	noStyle := tcell.StyleDefault.Reverse(true)
	if s.selectedOptionIdx == 1 {
		noStyle = tcell.StyleDefault.Foreground(tcell.ColorBlack).Background(tcell.ColorWhite).Reverse(true)
	}
	screen.SetContent(noX, yesNoY, '[', nil, noStyle)
	if s.selectedOptionIdx == 1 {
		screen.SetContent(noX+1, yesNoY, 'X', nil, noStyle)
	} else {
		screen.SetContent(noX+1, yesNoY, ' ', nil, noStyle)
	}
	screen.SetContent(noX+2, yesNoY, ']', nil, noStyle)
	for i, ch := range " No" {
		screen.SetContent(noX+3+i, yesNoY, ch, nil, noStyle)
	}

	// Draw instructions
	instructions := "←→: Toggle  Enter: Confirm  Esc: Cancel"
	instX := dialogX + (dialogWidth-len(instructions))/2
	instY := dialogY + dialogHeight - 2
	for i, ch := range instructions {
		if instX+i < dialogX+dialogWidth-1 {
			screen.SetContent(instX+i, instY, ch, nil, tcell.StyleDefault.Dim(true).Reverse(true))
		}
	}
}

// performCreatePartition creates the partition based on form data
func (s *tuiState) performCreatePartition() {
	form := &s.createPartitionForm

	// Parse form data and create partition
	err := createPartition(form.diskPath, form.unusedPartition, form.fields)
	if err != nil {
		s.showError = true
		s.errorMessage = fmt.Sprintf("Failed to create partition: %v", err)
		s.showCreatePartitionForm = false
		s.showCreatePartitionConfirm = false
		return
	}

	// Reload partitions
	partitions, err := getPartitionsData(s.currentDisk)
	if err != nil {
		s.showError = true
		s.errorMessage = fmt.Sprintf("Partition created but failed to refresh: %v", err)
	} else {
		s.partitions = partitions
		// Adjust selected index
		if s.selectedPartitionIdx >= len(s.partitions) {
			s.selectedPartitionIdx = len(s.partitions) - 1
		}
		if s.selectedPartitionIdx < 0 {
			s.selectedPartitionIdx = 0
		}
	}

	s.showCreatePartitionForm = false
	s.showCreatePartitionConfirm = false
	s.createPartitionForm = PartitionCreateForm{}
}

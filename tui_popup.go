package main

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
)

// renderPopup renders the options popup
func (s *tuiState) renderPopup(screen tcell.Screen, width, height int) {
	// Calculate popup size and position
	popupWidth := 50
	popupHeight := len(s.popupOptions) + 5 // +1 for title, +1 for separator, +1 for instructions, +2 for borders
	popupX := (width - popupWidth) / 2
	popupY := (height - popupHeight) / 2

	// Ensure popup fits on screen
	if popupX < 0 {
		popupX = 0
	}
	if popupY < 0 {
		popupY = 0
	}
	if popupX+popupWidth > width {
		popupWidth = width - popupX
	}
	if popupY+popupHeight > height {
		popupHeight = height - popupY
	}

	// Draw semi-transparent overlay
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			screen.SetContent(x, y, ' ', nil, tcell.StyleDefault.Dim(true))
		}
	}

	// Draw popup border and background using box drawing characters
	for y := popupY; y < popupY+popupHeight; y++ {
		for x := popupX; x < popupX+popupWidth; x++ {
			if y == popupY {
				// Top border
				if x == popupX {
					screen.SetContent(x, y, '┌', nil, tcell.StyleDefault.Bold(true))
				} else if x == popupX+popupWidth-1 {
					screen.SetContent(x, y, '┐', nil, tcell.StyleDefault.Bold(true))
				} else {
					screen.SetContent(x, y, '─', nil, tcell.StyleDefault.Bold(true))
				}
			} else if y == popupY+popupHeight-1 {
				// Bottom border
				if x == popupX {
					screen.SetContent(x, y, '└', nil, tcell.StyleDefault.Bold(true))
				} else if x == popupX+popupWidth-1 {
					screen.SetContent(x, y, '┘', nil, tcell.StyleDefault.Bold(true))
				} else {
					screen.SetContent(x, y, '─', nil, tcell.StyleDefault.Bold(true))
				}
			} else if x == popupX || x == popupX+popupWidth-1 {
				// Side borders
				screen.SetContent(x, y, '│', nil, tcell.StyleDefault.Bold(true))
			} else {
				// Background
				screen.SetContent(x, y, ' ', nil, tcell.StyleDefault.Reverse(true))
			}
		}
	}

	// Draw title
	title := "Partition Options"
	titleX := popupX + (popupWidth-len(title))/2
	for i, ch := range title {
		if titleX+i < popupX+popupWidth-1 {
			screen.SetContent(titleX+i, popupY+1, ch, nil, tcell.StyleDefault.Bold(true).Reverse(true))
		}
	}

	// Draw separator line
	sepY := popupY + 2
	screen.SetContent(popupX, sepY, '├', nil, tcell.StyleDefault.Bold(true))
	for x := popupX + 1; x < popupX+popupWidth-1; x++ {
		screen.SetContent(x, sepY, '─', nil, tcell.StyleDefault.Bold(true))
	}
	screen.SetContent(popupX+popupWidth-1, sepY, '┤', nil, tcell.StyleDefault.Bold(true))

	// Draw options
	optionY := popupY + 3
	for i, option := range s.popupOptions {
		if optionY+i >= popupY+popupHeight-2 {
			break // Don't draw beyond the instructions line
		}
		optionX := popupX + 2
		var style tcell.Style
		if i == s.selectedOptionIdx {
			style = tcell.StyleDefault.
				Foreground(tcell.ColorBlack).
				Background(tcell.ColorWhite).
				Reverse(true)
		} else {
			style = tcell.StyleDefault.Reverse(true)
		}

		// Draw selection marker
		if i == s.selectedOptionIdx {
			screen.SetContent(optionX, optionY+i, '▶', nil, style)
			optionX++
		} else {
			screen.SetContent(optionX, optionY+i, ' ', nil, style)
			optionX++
		}

		// Draw option text
		for j, ch := range option {
			if optionX+j < popupX+popupWidth-2 {
				screen.SetContent(optionX+j, optionY+i, ch, nil, style)
			}
		}
	}

	// Draw instructions (on a separate line above bottom border)
	instructions := "↑↓: Select  Enter: Choose  Esc: Cancel"
	if len(instructions) > popupWidth-4 {
		instructions = "↑↓: Select  Enter: Choose  Esc: Cancel"
		// Truncate if still too long
		if len(instructions) > popupWidth-4 {
			instructions = instructions[:popupWidth-4]
		}
	}
	instX := popupX + (popupWidth-len(instructions))/2
	instY := popupY + popupHeight - 2
	for i, ch := range instructions {
		if instX+i < popupX+popupWidth-1 {
			screen.SetContent(instX+i, instY, ch, nil, tcell.StyleDefault.Dim(true).Reverse(true))
		}
	}
}

// renderConfirmDialog renders the confirmation dialog
func (s *tuiState) renderConfirmDialog(screen tcell.Screen, width, height int) {
	// Calculate dialog size and position
	dialogWidth := 60
	dialogHeight := 7
	dialogX := (width - dialogWidth) / 2
	dialogY := (height - dialogHeight) / 2

	// Draw semi-transparent overlay
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			screen.SetContent(x, y, ' ', nil, tcell.StyleDefault.Dim(true))
		}
	}

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

	// Draw dialog border and background using box drawing characters
	for y := dialogY; y < dialogY+dialogHeight; y++ {
		for x := dialogX; x < dialogX+dialogWidth; x++ {
			if y == dialogY {
				// Top border
				if x == dialogX {
					screen.SetContent(x, y, '┌', nil, tcell.StyleDefault.Bold(true))
				} else if x == dialogX+dialogWidth-1 {
					screen.SetContent(x, y, '┐', nil, tcell.StyleDefault.Bold(true))
				} else {
					screen.SetContent(x, y, '─', nil, tcell.StyleDefault.Bold(true))
				}
			} else if y == dialogY+dialogHeight-1 {
				// Bottom border
				if x == dialogX {
					screen.SetContent(x, y, '└', nil, tcell.StyleDefault.Bold(true))
				} else if x == dialogX+dialogWidth-1 {
					screen.SetContent(x, y, '┘', nil, tcell.StyleDefault.Bold(true))
				} else {
					screen.SetContent(x, y, '─', nil, tcell.StyleDefault.Bold(true))
				}
			} else if x == dialogX || x == dialogX+dialogWidth-1 {
				// Side borders
				screen.SetContent(x, y, '│', nil, tcell.StyleDefault.Bold(true))
			} else {
				// Background
				screen.SetContent(x, y, ' ', nil, tcell.StyleDefault.Reverse(true))
			}
		}
	}

	// Draw message
	msg := s.confirmMessage
	msgX := dialogX + (dialogWidth-len(msg))/2
	msgY := dialogY + 2
	for i, ch := range msg {
		if msgX+i < dialogX+dialogWidth-1 {
			screen.SetContent(msgX+i, msgY, ch, nil, tcell.StyleDefault.Reverse(true))
		}
	}

	// Draw Yes/No options
	yesNoY := dialogY + 4
	yesX := dialogX + dialogWidth/2 - 10
	noX := dialogX + dialogWidth/2 + 5

	// Draw "Yes"
	yesStyle := tcell.StyleDefault.Reverse(true)
	if s.selectedOptionIdx == 0 {
		yesStyle = tcell.StyleDefault.
			Foreground(tcell.ColorBlack).
			Background(tcell.ColorWhite).
			Reverse(true)
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

	// Draw "No"
	noStyle := tcell.StyleDefault.Reverse(true)
	if s.selectedOptionIdx == 1 {
		noStyle = tcell.StyleDefault.
			Foreground(tcell.ColorBlack).
			Background(tcell.ColorWhite).
			Reverse(true)
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

	// Draw instructions (on a separate line above bottom border)
	instructions := "←→: Toggle  Enter: Confirm  Esc: Cancel"
	if len(instructions) > dialogWidth-4 {
		instructions = "←→: Toggle  Enter: Confirm  Esc: Cancel"
		// Truncate if still too long
		if len(instructions) > dialogWidth-4 {
			instructions = instructions[:dialogWidth-4]
		}
	}
	instX := dialogX + (dialogWidth-len(instructions))/2
	instY := dialogY + dialogHeight - 2
	for i, ch := range instructions {
		if instX+i < dialogX+dialogWidth-1 {
			screen.SetContent(instX+i, instY, ch, nil, tcell.StyleDefault.Dim(true).Reverse(true))
		}
	}
}

// performDelete deletes the selected partition
func (s *tuiState) performDelete() {
	if len(s.partitions) == 0 || s.selectedPartitionIdx < 0 || s.selectedPartitionIdx >= len(s.partitions) {
		return
	}

	selectedPart := s.partitions[s.selectedPartitionIdx]
	if selectedPart.Unused {
		return // Can't delete unused space
	}

	// Delete the partition
	err := deletePartition(s.currentDisk, selectedPart)
	if err != nil {
		// Show error message to user
		s.showError = true
		s.errorMessage = fmt.Sprintf("Failed to delete partition: %v", err)
		return
	}

	// Reload partitions
	partitions, err := getPartitionsData(s.currentDisk)
	if err != nil {
		s.showError = true
		s.errorMessage = fmt.Sprintf("Partition deleted but failed to refresh: %v", err)
		// Still try to continue
	} else {
		s.partitions = partitions

		// Adjust selected index if needed
		if s.selectedPartitionIdx >= len(s.partitions) {
			s.selectedPartitionIdx = len(s.partitions) - 1
		}
		if s.selectedPartitionIdx < 0 {
			s.selectedPartitionIdx = 0
		}
	}
}

// renderErrorDialog renders an error message dialog
func (s *tuiState) renderErrorDialog(screen tcell.Screen, width, height int) {
	// Calculate dialog size and position
	errorMsg := s.errorMessage
	// Wrap message if too long
	maxWidth := 70
	if len(errorMsg) > maxWidth {
		// Simple word wrap
		words := strings.Fields(errorMsg)
		var lines []string
		var currentLine string
		for _, word := range words {
			if len(currentLine)+len(word)+1 <= maxWidth {
				if currentLine != "" {
					currentLine += " "
				}
				currentLine += word
			} else {
				if currentLine != "" {
					lines = append(lines, currentLine)
				}
				currentLine = word
			}
		}
		if currentLine != "" {
			lines = append(lines, currentLine)
		}
		errorMsg = strings.Join(lines, "\n")
	}

	lines := strings.Split(errorMsg, "\n")
	dialogWidth := 70
	if len(errorMsg) < dialogWidth {
		dialogWidth = len(errorMsg) + 4
	}
	dialogHeight := len(lines) + 5
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

	// Draw dialog border and background
	for y := dialogY; y < dialogY+dialogHeight; y++ {
		for x := dialogX; x < dialogX+dialogWidth; x++ {
			if y == dialogY {
				// Top border
				if x == dialogX {
					screen.SetContent(x, y, '┌', nil, tcell.StyleDefault.Bold(true))
				} else if x == dialogX+dialogWidth-1 {
					screen.SetContent(x, y, '┐', nil, tcell.StyleDefault.Bold(true))
				} else {
					screen.SetContent(x, y, '─', nil, tcell.StyleDefault.Bold(true))
				}
			} else if y == dialogY+dialogHeight-1 {
				// Bottom border
				if x == dialogX {
					screen.SetContent(x, y, '└', nil, tcell.StyleDefault.Bold(true))
				} else if x == dialogX+dialogWidth-1 {
					screen.SetContent(x, y, '┘', nil, tcell.StyleDefault.Bold(true))
				} else {
					screen.SetContent(x, y, '─', nil, tcell.StyleDefault.Bold(true))
				}
			} else if x == dialogX || x == dialogX+dialogWidth-1 {
				// Side borders
				screen.SetContent(x, y, '│', nil, tcell.StyleDefault.Bold(true))
			} else {
				// Background
				screen.SetContent(x, y, ' ', nil, tcell.StyleDefault.Reverse(true))
			}
		}
	}

	// Draw title
	title := "Error"
	titleX := dialogX + (dialogWidth-len(title))/2
	for i, ch := range title {
		if titleX+i < dialogX+dialogWidth-1 {
			screen.SetContent(titleX+i, dialogY+1, ch, nil, tcell.StyleDefault.Bold(true).Foreground(tcell.ColorRed).Reverse(true))
		}
	}

	// Draw error message
	msgY := dialogY + 3
	for i, line := range lines {
		if msgY+i >= dialogY+dialogHeight-2 {
			break
		}
		lineX := dialogX + (dialogWidth-len(line))/2
		for j, ch := range line {
			if lineX+j < dialogX+dialogWidth-1 {
				screen.SetContent(lineX+j, msgY+i, ch, nil, tcell.StyleDefault.Reverse(true))
			}
		}
	}

	// Draw instructions
	instructions := "Press any key to close"
	instX := dialogX + (dialogWidth-len(instructions))/2
	instY := dialogY + dialogHeight - 2
	for i, ch := range instructions {
		if instX+i < dialogX+dialogWidth-1 {
			screen.SetContent(instX+i, instY, ch, nil, tcell.StyleDefault.Dim(true).Reverse(true))
		}
	}
}

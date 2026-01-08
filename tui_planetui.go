package main

import (
	"fmt"
	"log"

	"github.com/chzyer/readline"
	tui "github.com/network-plane/planetui"
)

// tuiInteractiveFactory creates a command factory for the interactive TUI
type tuiInteractiveFactory struct {
	spec tui.CommandSpec
}

// tuiInteractiveCommand implements the interactive TUI command
type tuiInteractiveCommand struct {
	spec tui.CommandSpec
}

func newTUIInteractiveFactory() tui.CommandFactory {
	spec := tui.CommandSpec{
		Name:        "interactive",
		Summary:     "Launch interactive full-screen TUI",
		Description: "Launches an interactive full-screen terminal UI for browsing disks and partitions.",
		Context:     "disk",
		Aliases:     []string{"i", "tui"},
	}
	return &tuiInteractiveFactory{spec: spec}
}

func (f *tuiInteractiveFactory) Spec() tui.CommandSpec { return f.spec }

func (f *tuiInteractiveFactory) New(rt tui.CommandRuntime) (tui.Command, error) {
	return &tuiInteractiveCommand{spec: f.spec}, nil
}

func (c *tuiInteractiveCommand) Spec() tui.CommandSpec { return c.spec }

func (c *tuiInteractiveCommand) Execute(rt tui.CommandRuntime, input tui.CommandInput) tui.CommandResult {
	// Launch the full-screen TUI
	runTUI()
	return tui.CommandResult{Status: tui.StatusSuccess}
}

// tuiListDisksFactory creates a command factory for listing disks
type tuiListDisksFactory struct {
	spec tui.CommandSpec
}

// tuiListDisksCommand implements the list disks command
type tuiListDisksCommand struct {
	spec tui.CommandSpec
}

func newTUIListDisksFactory() tui.CommandFactory {
	spec := tui.CommandSpec{
		Name:        "list",
		Summary:     "List available disks",
		Description: "Lists all available disks on the system.",
		Context:     "disk",
		Aliases:     []string{"ls", "disks"},
	}
	return &tuiListDisksFactory{spec: spec}
}

func (f *tuiListDisksFactory) Spec() tui.CommandSpec { return f.spec }

func (f *tuiListDisksFactory) New(rt tui.CommandRuntime) (tui.Command, error) {
	return &tuiListDisksCommand{spec: f.spec}, nil
}

func (c *tuiListDisksCommand) Spec() tui.CommandSpec { return c.spec }

func (c *tuiListDisksCommand) Execute(rt tui.CommandRuntime, input tui.CommandInput) tui.CommandResult {
	disks := getDiskList()
	if len(disks) == 0 {
		rt.Output().Warn("No disks found")
		return tui.CommandResult{Status: tui.StatusSuccess}
	}

	// Output disks in a table format
	for i, disk := range disks {
		diskTypeLabel := ""
		switch disk.DiskType {
		case "synthesized":
			diskTypeLabel = " [synthesized]"
		case "image":
			diskTypeLabel = " [image]"
		case "physical":
			diskTypeLabel = " [physical]"
		}
		rt.Output().Info(fmt.Sprintf("%d. %s%s", i+1, disk.Path, diskTypeLabel))
	}

	return tui.CommandResult{Status: tui.StatusSuccess}
}

// tuiSelectDiskFactory creates a command factory for selecting a disk
type tuiSelectDiskFactory struct {
	spec tui.CommandSpec
}

// tuiSelectDiskCommand implements the select disk command
type tuiSelectDiskCommand struct {
	spec tui.CommandSpec
}

func newTUISelectDiskFactory() tui.CommandFactory {
	spec := tui.CommandSpec{
		Name:        "select",
		Summary:     "Select a disk to view partitions",
		Description: "Selects a disk and navigates to partition view.",
		Context:     "disk",
		Aliases:     []string{"sel", "disk"},
		Args: []tui.ArgSpec{
			{Name: "disk", Type: tui.ArgTypeString, Required: true, Description: "Disk path to select"},
		},
	}
	return &tuiSelectDiskFactory{spec: spec}
}

func (f *tuiSelectDiskFactory) Spec() tui.CommandSpec { return f.spec }

func (f *tuiSelectDiskFactory) New(rt tui.CommandRuntime) (tui.Command, error) {
	return &tuiSelectDiskCommand{spec: f.spec}, nil
}

func (c *tuiSelectDiskCommand) Spec() tui.CommandSpec { return c.spec }

func (c *tuiSelectDiskCommand) Execute(rt tui.CommandRuntime, input tui.CommandInput) tui.CommandResult {
	diskPath := input.Args.String("disk")
	if diskPath == "" {
		rt.Output().Error("Disk path is required")
		return tui.CommandResult{
			Status: tui.StatusSuccess,
			Error:  &tui.CommandError{Message: "disk path required"},
		}
	}

	// Store selected disk in session
	rt.Session().Set("selected_disk", diskPath)

	// Navigate to partition context
	rt.NavigateTo("partition", nil)

	rt.Output().Info(fmt.Sprintf("Selected disk: %s", diskPath))
	return tui.CommandResult{Status: tui.StatusSuccess}
}

// tuiListPartitionsFactory creates a command factory for listing partitions
type tuiListPartitionsFactory struct {
	spec tui.CommandSpec
}

// tuiListPartitionsCommand implements the list partitions command
type tuiListPartitionsCommand struct {
	spec tui.CommandSpec
}

func newTUIListPartitionsFactory() tui.CommandFactory {
	spec := tui.CommandSpec{
		Name:        "list",
		Summary:     "List partitions on selected disk",
		Description: "Lists all partitions on the currently selected disk.",
		Context:     "partition",
		Aliases:     []string{"ls", "partitions"},
	}
	return &tuiListPartitionsFactory{spec: spec}
}

func (f *tuiListPartitionsFactory) Spec() tui.CommandSpec { return f.spec }

func (f *tuiListPartitionsFactory) New(rt tui.CommandRuntime) (tui.Command, error) {
	return &tuiListPartitionsCommand{spec: f.spec}, nil
}

func (c *tuiListPartitionsCommand) Spec() tui.CommandSpec { return c.spec }

func (c *tuiListPartitionsCommand) Execute(rt tui.CommandRuntime, input tui.CommandInput) tui.CommandResult {
	// Get selected disk from session
	diskPathVal, ok := rt.Session().Get("selected_disk")
	if !ok || diskPathVal == nil {
		rt.Output().Error("No disk selected. Use 'select <disk>' first.")
		return tui.CommandResult{
			Status: tui.StatusSuccess,
			Error:  &tui.CommandError{Message: "no disk selected"},
		}
	}

	diskPath, ok := diskPathVal.(string)
	if !ok {
		rt.Output().Error("Invalid disk selection")
		return tui.CommandResult{
			Status: tui.StatusSuccess,
			Error:  &tui.CommandError{Message: "invalid disk selection"},
		}
	}

	partitions, err := getPartitionsData(diskPath)
	if err != nil {
		rt.Output().Error(fmt.Sprintf("Failed to get partitions: %v", err))
		return tui.CommandResult{
			Status: tui.StatusSuccess,
			Error:  &tui.CommandError{Message: err.Error()},
		}
	}

	if len(partitions) == 0 {
		rt.Output().Warn(fmt.Sprintf("No partitions found on %s", diskPath))
		return tui.CommandResult{Status: tui.StatusSuccess}
	}

	// Output partitions
	for _, part := range partitions {
		if part.Unused {
			rt.Output().Info(fmt.Sprintf("Unused space: %s (LBA %d - %d)", part.Size, part.FirstLBA, part.LastLBA))
		} else {
			rt.Output().Info(fmt.Sprintf("Partition %d: %s (%s) - %s", part.Number, part.Name, part.Type, part.Size))
		}
	}

	return tui.CommandResult{Status: tui.StatusSuccess}
}

// initPlanetUI initializes planetui with commands and contexts
func initPlanetUI() {
	// Register contexts
	tui.RegisterContext("disk", "Disk management commands")
	tui.RegisterContext("partition", "Partition management commands")

	// Register commands
	tui.RegisterCommand(newTUIInteractiveFactory())
	tui.RegisterCommand(newTUIListDisksFactory())
	tui.RegisterCommand(newTUISelectDiskFactory())
	tui.RegisterCommand(newTUIListPartitionsFactory())
}

// runPlanetUITUI runs the planetui-based TUI
func runPlanetUITUI() {
	initPlanetUI()

	rl, err := readline.NewEx(&readline.Config{
		Prompt:          "dsktool> ",
		HistoryFile:     "/tmp/dsktool_history",
		AutoComplete:    nil,
		InterruptPrompt: "^C",
		EOFPrompt:       "exit",
	})
	if err != nil {
		log.Fatal(err)
	}
	defer rl.Close()

	// Start in disk context
	if err := tui.Run(rl); err != nil {
		log.Fatal(err)
	}
}

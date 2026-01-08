// Package main provides dsktool, a comprehensive disk management utility.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	rootCmd = &cobra.Command{
		Use:     "dsktool",
		Short:   "Earentir Disk Tools",
		Long:    "Earentir Disk Tools - A comprehensive disk management utility",
		Version: appversion,
	}
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	// List disks command
	var listDisksCmd = &cobra.Command{
		Use:     "disks",
		Aliases: []string{"d", "disk"},
		Short:   "List Disks",
		Long:    "List all available disks on the system",
		Run: func(_ *cobra.Command, _ []string) {
			listDisks()
		},
	}

	// List partitions command
	var listPartitionsCmd = &cobra.Command{
		Use:     "partitions",
		Aliases: []string{"p", "part"},
		Short:   "List Partitions",
		Long:    "List partitions on a specified disk device",
		Args:    cobra.ExactArgs(1),
		Run: func(_ *cobra.Command, args []string) {
			deviceToRead := args[0]
			checkForPerms(deviceToRead)
			listPartitions(deviceToRead)
		},
	}

	// List bytes command
	var listBytesCmd = &cobra.Command{
		Use:     "list",
		Aliases: []string{"l"},
		Short:   "List bytes from disk",
		Long:    "Read and display bytes from a disk device",
		Args:    cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			deviceToRead := args[0]
			checkForPerms(deviceToRead)
			bytes, _ := cmd.Flags().GetInt("bytes")
			offset, _ := cmd.Flags().GetInt("offset")
			// This is not good, we cant use an offset larger than 2^32
			printDiskBytes(deviceToRead, bytes, int64(offset))
		},
	}
	listBytesCmd.Flags().Int("bytes", 512, "Number of bytes to read")
	listBytesCmd.Flags().Int("offset", 0, "Offset to start reading from")

	// Benchmark command
	var benchmarkCmd = &cobra.Command{
		Use:     "benchmark",
		Aliases: []string{"b", "bench"},
		Short:   "Benchmark Disk",
		Long:    "Run disk benchmark tests",
		Run: func(cmd *cobra.Command, _ []string) {
			size, _ := cmd.Flags().GetInt("size")
			dir, _ := cmd.Flags().GetString("dir")
			iterations, _ := cmd.Flags().GetInt("iterations")
			checkForPerms(dir)
			benchFullTest(size, iterations, dir)
		},
	}
	benchmarkCmd.Flags().Int("size", 1024, "Size of the file to write in MB")
	benchmarkCmd.Flags().String("dir", ".", "Directory to write the file to")
	benchmarkCmd.Flags().Int("iterations", 5, "Number of iterations to run")

	// Image command
	var imageCmd = &cobra.Command{
		Use:     "image",
		Aliases: []string{"i"},
		Short:   "Image A Disk",
		Long:    "Create a disk image from a device",
		Args:    cobra.RangeArgs(1, 2),
		Run: func(cmd *cobra.Command, args []string) {
			deviceToRead := args[0]
			outputfile := "diskimage"
			if len(args) > 1 {
				outputfile = args[1]
			}

			// Exit if we don't have permission to read the device
			if !hasReadPermission(deviceToRead) {
				fmt.Printf("No permission to read the device: %s, try with elevated priviledges\n", deviceToRead)
				os.Exit(13)
			}

			compress, _ := cmd.Flags().GetString("compress")
			if compress == "" {
				compress = "gzip"
			}

			readdisk(deviceToRead, outputfile, compress)
		},
	}
	imageCmd.Flags().String("compress", "gzip", "Compression method to use (gzip, bzip2, zip, snappy, s2, zlib, zstd)")

	// TUI command - uses planetui
	var tuiCmd = &cobra.Command{
		Use:     "tui",
		Aliases: []string{"interactive"},
		Short:   "Interactive TUI disk selector",
		Long:    "Interactive terminal UI for browsing disks and partitions. Use 'interactive' command for full-screen mode.",
		Run: func(_ *cobra.Command, _ []string) {
			runPlanetUITUI()
		},
	}

	// Add all commands to root
	rootCmd.AddCommand(listDisksCmd)
	rootCmd.AddCommand(listPartitionsCmd)
	rootCmd.AddCommand(listBytesCmd)
	rootCmd.AddCommand(benchmarkCmd)
	rootCmd.AddCommand(imageCmd)
	rootCmd.AddCommand(tuiCmd)
}

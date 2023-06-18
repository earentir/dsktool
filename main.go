package main

import (
	"fmt"
	"os"

	cli "github.com/jawher/mow.cli"
)

var (
	sectorSize uint64
	appversion = "0.2.11"
)

// Windows is not tested at all, please be ware
func main() {

	app := cli.App("disktool", "Various Disk Tools")
	app.Version("v version", appversion)

	app.Command("l list", "List the first 512 bytes of the disk", func(cmd *cli.Cmd) {
		cmd.Spec = "DEVICE"
		deviceToRead := cmd.StringArg("DEVICE", "", "Disk To Use")

		cmd.Action = func() {
			checkForPerms(*deviceToRead)
			printDiskBytes(*deviceToRead, 512, 0)
		}
	})

	app.Command("p part partitions", "List Partitions", func(cmd *cli.Cmd) {
		cmd.Spec = "DEVICE"
		deviceToRead := cmd.StringArg("DEVICE", "", "Disk To Use")

		cmd.Action = func() {
			checkForPerms(*deviceToRead)
			listPartitions(*deviceToRead)
		}
	})

	app.Command("d disk disks", "List Disks", func(cmd *cli.Cmd) {
		cmd.Action = func() {
			listDisks()
		}
	})

	app.Command("i image", "Image A Disk", func(cmd *cli.Cmd) {
		cmd.Spec = "DEVICE OUTPUTFILE [--gzip | --bzip2 | --zip | --snappy | --zlib | --zstd]"

		var (
			deviceToRead = cmd.StringArg("DEVICE", "", "Disk To Use")
			outputfile   = cmd.StringArg("OUTPUTFILE", "sda.gz", "File to write the Image into")
			gzip         = cmd.BoolOpt("gzip", true, "gzip")
			bzip         = cmd.BoolOpt("bzip2", false, "bzip2")
			zstd         = cmd.BoolOpt("zstd", false, "zstd")
			snappy       = cmd.BoolOpt("snappy", false, "snappy")
			zlib         = cmd.BoolOpt("zlib", false, "zlib")
			zip          = cmd.BoolOpt("zip", false, "zip")
		)

		cmd.Action = func() {
			//Exit if we don't have permission to read the device
			if !hasReadPermission(*deviceToRead) {
				fmt.Printf("No permission to read the device: %s, try with elevated priviledges\n", *deviceToRead)
				os.Exit(13)
			}

			compressMethods := map[string]*bool{
				"gzip":   gzip,
				"zlib":   zlib,
				"bzip2":  bzip,
				"snappy": snappy,
				"zstd":   zstd,
				"zip":    zip,
			}

			selectedMethods := make([]string, 0)
			for method, flag := range compressMethods {
				if *flag {
					selectedMethods = append(selectedMethods, method)
				}
			}

			if len(selectedMethods) > 1 {
				fmt.Println("You can only use one compression method")
				os.Exit(1)
			}

			if len(selectedMethods) == 1 {
				readdisk(*deviceToRead, *outputfile, selectedMethods[0])
			}
		}
	})

	err := app.Run(os.Args)
	if err != nil {
		fmt.Println(err.Error())
	}
}

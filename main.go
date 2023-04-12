package main

import (
	"fmt"
	"os"

	cli "github.com/jawher/mow.cli"
)

var (
	sectorSize   uint64
	appversion   = "0.2.5"
	deviceToRead *string
)

func main() {

	app := cli.App("disktool", "Various Disk Tools")
	app.Spec = "DEVICE"
	app.Version("v version", appversion)

	var (
		deviceToRead = app.StringArg("DEVICE", "/dev/sda", "Disk To Use")
	)

	app.Command("l list", "List the first 512 bytes of the disk", func(cmd *cli.Cmd) {
		cmd.Action = func() {
			printDiskBytes(*deviceToRead, 512)
		}
	})

	app.Command("p part partitions", "List Partitions", func(cmd *cli.Cmd) {
		cmd.Action = func() {
			listPartitions(*deviceToRead)
		}
	})

	app.Command("d disk disks", "List Disks", func(cmd *cli.Cmd) {
		cmd.Action = func() {
			listDisks()
		}
	})

	app.Command("i image", "Image A Disk", func(cmd *cli.Cmd) {
		cmd.Spec = "OUTPUTFILE"

		var (
			outputfile = cmd.StringArg("OUTPUTFILE", "sda.gz", "File to write the Image into")
		)

		cmd.Action = func() {
			readdiskLinux(*deviceToRead, *outputfile)
		}
	})

	err := app.Run(os.Args)
	if err != nil {
		fmt.Println(err.Error())
	}
}

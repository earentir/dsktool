package main

import (
	"fmt"
	"os"

	cli "github.com/jawher/mow.cli"
)

var (
	sectorSize uint64
	appversion = "0.2.9"
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
		cmd.Spec = "OUTPUTFILE [-g | -b | -z | -s | -l]"

		var (
			outputfile = cmd.StringArg("OUTPUTFILE", "sda.gz", "File to write the Image into")
			gzip       = cmd.BoolOpt("g", true, "gzip (Default)")
			bzip       = cmd.BoolOpt("b", false, "bzip2")
			zstd       = cmd.BoolOpt("z", false, "zstd the Image")
			snappy     = cmd.BoolOpt("s", false, "snappy the Image")
			zlib       = cmd.BoolOpt("l", false, "zlib the Image")
		)

		cmd.Action = func() {
			if *gzip && *bzip && *zstd && *snappy && *zlib {
				fmt.Println("You can only use one compression method")
				os.Exit(1)
			}

			if *gzip {
				readdisk(*deviceToRead, *outputfile, "gzip")
			}
			if *bzip {
				readdisk(*deviceToRead, *outputfile, "bzip2")
			}
			if *zstd {
				readdisk(*deviceToRead, *outputfile, "zstd")
			}
			if *snappy {
				readdisk(*deviceToRead, *outputfile, "snappy")
			}
			if *zlib {
				readdisk(*deviceToRead, *outputfile, "zlib")
			}
		}
	})

	err := app.Run(os.Args)
	if err != nil {
		fmt.Println(err.Error())
	}
}

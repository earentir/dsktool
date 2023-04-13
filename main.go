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
			gzip       = cmd.BoolOpt("g", true, "gzip")
			bzip       = cmd.BoolOpt("b", false, "bzip2")
			zstd       = cmd.BoolOpt("t", false, "zstd")
			snappy     = cmd.BoolOpt("s", false, "snappy")
			zlib       = cmd.BoolOpt("l", false, "zlib")
			zip        = cmd.BoolOpt("z", false, "zip")
		)

		cmd.Action = func() {
			if *gzip && *bzip && *zstd && *snappy && *zlib && *zip {
				fmt.Println("You can only use one compression method")
				os.Exit(1)
			}

			if *gzip {
				readdisk(*deviceToRead, *outputfile, "gzip")
			}
			if *zlib {
				readdisk(*deviceToRead, *outputfile, "zlib")
			}
			if *bzip {
				readdisk(*deviceToRead, *outputfile, "bzip2")
			}
			if *snappy {
				readdisk(*deviceToRead, *outputfile, "snappy")
			}
			if *zstd {
				readdisk(*deviceToRead, *outputfile, "zstd")
			}
			if *zip {
				readdisk(*deviceToRead, *outputfile, "zstd")
			}
		}
	})

	err := app.Run(os.Args)
	if err != nil {
		fmt.Println(err.Error())
	}
}

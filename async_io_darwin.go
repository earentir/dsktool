//go:build darwin

package main

import (
	"os"
)

func asyncWrite(f *os.File, buf []byte, offset int64) error {
	_, err := f.WriteAt(buf, offset)
	return err
}

func asyncRead(f *os.File, buf []byte, offset int64) error {
	_, err := f.ReadAt(buf, offset)
	return err
}

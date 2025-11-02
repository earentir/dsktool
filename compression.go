package main

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/dsnet/compress/bzip2"
	"github.com/gosuri/uilive"
	"github.com/klauspost/compress/gzip"
	"github.com/klauspost/compress/s2"
	"github.com/klauspost/compress/snappy"
	"github.com/klauspost/compress/zlib"
	"github.com/klauspost/compress/zstd"
)

type countingWriter struct {
	w     io.Writer
	count int64
}

func (cw *countingWriter) Write(p []byte) (int, error) {
	n, err := cw.w.Write(p)
	cw.count += int64(n)
	return n, err
}

// getCompressionExtension returns the file extension for a given compression algorithm
func getCompressionExtension(compressionAlgorithm string) (string, error) {
	switch compressionAlgorithm {
	case "gzip":
		return ".gz", nil
	case "zlib":
		return ".zlib", nil
	case "bzip2":
		return ".bz2", nil
	case "snappy":
		return ".snappy", nil
	case "s2":
		return ".s2", nil
	case "zstd":
		return ".zst", nil
	case "zip":
		return ".zip", nil
	default:
		return "", fmt.Errorf("unsupported compression algorithm: %s", compressionAlgorithm)
	}
}

// createCompressionWriter creates a compression writer based on the algorithm
// Returns the writer, a zip writer (if applicable), and an error
func createCompressionWriter(algorithm string, output io.Writer) (io.Writer, *zip.Writer, error) {
	switch algorithm {
	case "gzip":
		return gzip.NewWriter(output), nil, nil
	case "zlib":
		return zlib.NewWriter(output), nil, nil
	case "bzip2":
		writer, err := bzip2.NewWriter(output, &bzip2.WriterConfig{})
		return writer, nil, err
	case "snappy":
		return snappy.NewBufferedWriter(output), nil, nil
	case "s2":
		return s2.NewWriter(output), nil, nil
	case "zstd":
		writer, err := zstd.NewWriter(output)
		return writer, nil, err
	case "zip":
		zipWriter := zip.NewWriter(output)
		zipFile, err := zipWriter.Create("compressedData")
		if err != nil {
			_ = zipWriter.Close()
			return nil, nil, fmt.Errorf("failed to create zip entry: %w", err)
		}
		return zipFile, zipWriter, nil
	default:
		return nil, nil, fmt.Errorf("unsupported compression algorithm: %s", algorithm)
	}
}

// compressFromReader reads from a reader and compresses to a file with progress reporting
func compressFromReader(disk io.Reader, outputfile string, compressionAlgorithm string, totalSize int64) error {
	// Determine file extension
	extension, err := getCompressionExtension(compressionAlgorithm)
	if err != nil {
		return err
	}

	outputfile = outputfile + extension

	// Create output file
	output, err := os.Create(outputfile)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer func() {
		_ = output.Close()
	}()

	// Wrap output with a countingWriter
	cw := &countingWriter{w: output}

	// Create compression writer
	compressedWriter, zipWriter, err := createCompressionWriter(compressionAlgorithm, cw)
	if err != nil {
		return fmt.Errorf("failed to create compression writer: %w", err)
	}

	fmt.Printf("Writing to Image: %s\n", outputfile)

	start := time.Now()

	// Setup uilive for dynamic output
	writer := uilive.New()
	writer.Start()
	defer writer.Stop()

	var (
		bytesRead  int64
		byteCount  = 16384
		buf        = make([]byte, byteCount)
		lastUpdate = time.Now()
	)

	for {
		n, err := disk.Read(buf)
		if n > 0 {
			_, wErr := compressedWriter.Write(buf[:n])
			if wErr != nil {
				_, _ = fmt.Fprintln(writer.Bypass(), "Failed to write compressed stream:", wErr.Error())
				return wErr
			}

			bytesRead += int64(n)

			// Update once every second
			if time.Since(lastUpdate) >= time.Second {
				elapsed := time.Since(start).Truncate(time.Second)
				var estimateStr string
				if totalSize > 0 && bytesRead > 0 {
					elapsedSeconds := time.Since(start).Seconds()
					if elapsedSeconds > 0 {
						rate := float64(bytesRead) / elapsedSeconds
						remaining := float64(totalSize-bytesRead) / rate
						if remaining < 0 {
							remaining = 0
						}
						// Format as human-readable time
						if remaining < 60 {
							estimateStr = fmt.Sprintf("%.0fs", remaining)
						} else if remaining < 3600 {
							estimateStr = fmt.Sprintf("%.0fm%.0fs", remaining/60, float64(int(remaining)%60))
						} else {
							hours := int(remaining / 3600)
							mins := int((remaining - float64(hours)*3600) / 60)
							secs := int(remaining - float64(hours)*3600 - float64(mins)*60)
							estimateStr = fmt.Sprintf("%dh%dm%ds", hours, mins, secs)
						}
					} else {
						estimateStr = "N/A"
					}
				} else {
					estimateStr = "N/A"
				}

				elapsedSeconds := time.Since(start).Seconds()
				readBps := float64(bytesRead) / elapsedSeconds
				writeBps := float64(cw.count) / elapsedSeconds

				_, _ = fmt.Fprintf(writer,
					"Byte Count: Read: %s (%d bytes), Written: %s (%d bytes)\n",
					formatBytes(bytesRead), bytesRead,
					formatBytes(cw.count), cw.count)
				_, _ = fmt.Fprintf(writer, "Elapsed Time: %s\n", elapsed)
				_, _ = fmt.Fprintf(writer, "Estimated Time: %s\n", estimateStr)
				_, _ = fmt.Fprintf(writer, "Read Speed: %s\n", formatSpeed(readBps))
				_, _ = fmt.Fprintf(writer, "Write Speed: %s\n", formatSpeed(writeBps))

				_ = writer.Flush()
				lastUpdate = time.Now()
			}
		}

		if err != nil {
			if err == io.EOF {
				// Final update at the end
				elapsed := time.Since(start).Truncate(time.Second)
				var estimateStr string
				if totalSize > 0 && bytesRead > 0 {
					elapsedSeconds := time.Since(start).Seconds()
					if elapsedSeconds > 0 {
						rate := float64(bytesRead) / elapsedSeconds
						remaining := float64(totalSize-bytesRead) / rate
						if remaining < 0 {
							remaining = 0
						}
						// Format as human-readable time
						if remaining < 60 {
							estimateStr = fmt.Sprintf("%.0fs", remaining)
						} else if remaining < 3600 {
							estimateStr = fmt.Sprintf("%.0fm%.0fs", remaining/60, float64(int(remaining)%60))
						} else {
							hours := int(remaining / 3600)
							mins := int((remaining - float64(hours)*3600) / 60)
							secs := int(remaining - float64(hours)*3600 - float64(mins)*60)
							estimateStr = fmt.Sprintf("%dh%dm%ds", hours, mins, secs)
						}
					} else {
						estimateStr = "N/A"
					}
				} else {
					estimateStr = "N/A"
				}

				elapsedSeconds := time.Since(start).Seconds()
				readBps := float64(bytesRead) / elapsedSeconds
				writeBps := float64(cw.count) / elapsedSeconds

				_, _ = fmt.Fprintf(writer,
					"Byte Count: Read: %s (%d bytes), Written: %s (%d bytes)\n",
					formatBytes(bytesRead), bytesRead,
					formatBytes(cw.count), cw.count)
				_, _ = fmt.Fprintf(writer, "Elapsed Time: %s\n", elapsed)
				_, _ = fmt.Fprintf(writer, "Estimated Time: %s\n", estimateStr)
				_, _ = fmt.Fprintf(writer, "Read Speed: %s\n", formatSpeed(readBps))
				_, _ = fmt.Fprintf(writer, "Write Speed: %s\n", formatSpeed(writeBps))
				_ = writer.Flush()
				break
			}
			_, _ = fmt.Fprintln(writer.Bypass(), "Error reading from disk:", err.Error())
			return err
		}
	}

	totalBytes := bytesRead
	fmt.Println() // new line after finishing updates
	fmt.Println("Written:", formatBytes(totalBytes), "(", totalBytes, "bytes )")

	// Close zipWriter if we have one
	if zipWriter != nil {
		err := zipWriter.Close()
		if err != nil {
			return fmt.Errorf("failed to close zip writer: %w", err)
		}
	} else {
		// If the compression writer implements Close, call it
		if wc, ok := compressedWriter.(io.WriteCloser); ok {
			_ = wc.Close()
		}
	}

	finalElapsed := time.Since(start).Truncate(time.Second)
	finalElapsedSeconds := time.Since(start).Seconds()
	finalReadBps := float64(bytesRead) / finalElapsedSeconds
	finalWriteBps := float64(cw.count) / finalElapsedSeconds

	// Calculate compression ratio: original_size / compressed_size
	var compressionRatio string
	if cw.count > 0 {
		ratio := float64(totalBytes) / float64(cw.count)
		compressionRatio = fmt.Sprintf("%.2f:1", ratio)
	} else {
		compressionRatio = "N/A"
	}

	fmt.Printf("Total actual time: %s (%s read, %s write) Compression ratio: %s\n",
		finalElapsed, formatSpeed(finalReadBps), formatSpeed(finalWriteBps), compressionRatio)

	return nil
}

package main

import (
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/tinyrange/tinyrange/experimental/fs3"
)

type iso9660Node struct {
}

type iso9660Reader struct {
}

// IterateNodes implements fs3.FilesystemReader.
func (i *iso9660Reader) IterateNodes() fs3.NodeIterator {
	// The nodes that are returned are extended directory entries.
	// The basic idea is we iterate through the path table and then through each directory.
	panic("unimplemented")
}

// ReadContents implements fs3.FilesystemReader.
func (i *iso9660Reader) ReadContents(node fs3.Node) (fs3.ReaderHandle, error) {
	panic("unimplemented")
}

// ReadDirectory implements fs3.FilesystemReader.
func (i *iso9660Reader) ReadDirectory(node fs3.Node) (fs3.DirectoryEntryIterator, error) {
	panic("unimplemented")
}

var (
	_ fs3.FilesystemReader = &iso9660Reader{}
)

func OpenIso9660Image(reader io.ReaderAt) (fs3.FilesystemReader, error) {
	// Parse each volume descriptor.
	return nil, fmt.Errorf("not implemented")
}

var (
	inputFilename = flag.String("input", "", "the iso9660 image to read")
)

func appMain() error {
	flag.Parse()

	if *inputFilename == "" {
		return fmt.Errorf("input filename is required")
	}

	file, err := os.Open(*inputFilename)
	if err != nil {
		return fmt.Errorf("failed to open input file: %w", err)
	}
	defer file.Close()

	reader, err := OpenIso9660Image(file)
	if err != nil {
		return fmt.Errorf("failed to open iso9660 image: %w", err)
	}

	it := reader.IterateNodes()

	for {
		node, ok := it.Next()
		if !ok {
			break
		}

		switch node.Kind() {
		default:
			return fmt.Errorf("unexpected node kind: %v", node.Kind())
		}
	}

	return nil
}

func main() {
	if err := appMain(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

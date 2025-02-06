package main

import (
	"archive/tar"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime/pprof"
	"time"

	"github.com/tinyrange/tinyrange/pkg/archive2"
)

var (
	read       = flag.Bool("read", false, "read the archive")
	cpuprofile = flag.String("cpuprofile", "", "write cpu profile to file")
)

func appMain() error {
	flag.Parse()

	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			return fmt.Errorf("failed to create CPU profile: %w", err)
		}
		defer f.Close()

		if err := pprof.StartCPUProfile(f); err != nil {
			return fmt.Errorf("failed to start CPU profile: %w", err)
		}
		defer pprof.StopCPUProfile()
	}

	if !*read {
		if flag.NArg() != 1 {
			return fmt.Errorf("usage: %s <filename>", os.Args[0])
		}

		filename := flag.Arg(0)

		f, err := os.Open(filename)
		if err != nil {
			return fmt.Errorf("failed to open file: %w", err)
		}
		defer f.Close()

		reader := tar.NewReader(f)

		indexFile, err := os.Create(filename + ".index")
		if err != nil {
			return fmt.Errorf("failed to create index file: %w", err)
		}
		defer indexFile.Close()

		contentsFile, err := os.Create(filename + ".contents")
		if err != nil {
			return fmt.Errorf("failed to create contents file: %w", err)
		}
		defer contentsFile.Close()

		writer := archive2.NewArchiveWriter(indexFile, contentsFile)

		start := time.Now()

		for {
			header, err := reader.Next()
			if err == io.EOF {
				break
			} else if err != nil {
				return fmt.Errorf("failed to read tar header: %w", err)
			}

			var ent archive2.EntryFactory

			switch header.Typeflag {
			case tar.TypeReg:
				ent = ent.Kind(archive2.EntryKindRegular)
			case tar.TypeDir:
				ent = ent.Kind(archive2.EntryKindDirectory)
			case tar.TypeSymlink:
				ent = ent.Kind(archive2.EntryKindSymlink)
			case tar.TypeLink:
				ent = ent.Kind(archive2.EntryKindHardlink)
			case tar.TypeXGlobalHeader:
				continue
			default:
				return fmt.Errorf("unsupported tar entry type: %v", header.Typeflag)
			}

			ent = ent.Name(header.Name).
				Linkname(header.Linkname).
				Size(header.Size).
				Mode(header.FileInfo().Mode()).
				Owner(header.Uid, header.Gid).
				ModTime(header.ModTime)

			if err := writer.WriteEntry(ent, reader); err != nil {
				return fmt.Errorf("failed to write entry: %w", err)
			}
		}

		slog.Info("elapsed", "time", time.Since(start))

		return nil
	} else {
		if flag.NArg() != 1 {
			return fmt.Errorf("usage: %s -read <filename>", os.Args[0])
		}

		slog.Info("reading archive")

		filename := flag.Arg(0)

		indexFile, err := os.Open(filename + ".index")
		if err != nil {
			return fmt.Errorf("failed to open index file: %w", err)
		}
		defer indexFile.Close()

		contentsFile, err := os.Open(filename + ".contents")
		if err != nil {
			return fmt.Errorf("failed to open contents file: %w", err)
		}
		defer contentsFile.Close()

		reader := archive2.NewArchiveReader(indexFile, contentsFile)

		start := time.Now()

		total := 0

		for {
			err := reader.NextEntry()
			if err == io.EOF {
				break
			} else if err != nil {
				return fmt.Errorf("failed to read entry: %w", err)
			}

			total += int(reader.Kind())

			// slog.Info("entry",
			// 	"kind", reader.Kind(),
			// 	"name", reader.Name(),
			// 	"size", reader.Size(),
			// 	"mode", reader.Mode(),
			// 	"modTime", reader.ModTime(),
			// 	"hash", fmt.Sprintf("%x", reader.Hash()),
			// )

			// _ = ent
		}

		slog.Info("elapsed", "time", time.Since(start), "total", total)

		return nil
	}
}

func main() {
	if err := appMain(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

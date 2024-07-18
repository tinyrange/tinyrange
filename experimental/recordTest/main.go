package main

import (
	"flag"
	"io"
	"log/slog"
	"os"
	"runtime/pprof"
	"time"

	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/record"
)

var (
	input      = flag.String("input", "", "the input file to read")
	output     = flag.String("output", "", "the output file to write")
	cpuprofile = flag.String("cpuprofile", "", "write cpu profile to file")
)

func appMain() error {
	flag.Parse()

	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			return err
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	start := time.Now()

	records, err := record.ReadRecordsFromFile(filesystem.NewLocalFile(*input))
	if err != nil {
		return err
	}

	slog.Info("loaded records", "took", time.Since(start))

	start = time.Now()

	out, err := os.Create(*output)
	if err != nil {
		return err
	}

	writer := record.NewWriter2(out)

	for _, record := range records {
		if err := writer.WriteValue(record); err != nil {
			return err
		}
	}

	if err := out.Close(); err != nil {
		return err
	}

	slog.Info("written records2", "took", time.Since(start))

	start = time.Now()

	in, err := os.Open(*output)
	if err != nil {
		return err
	}
	defer in.Close()

	reader := record.NewReader2(in)

	for {
		value, err := reader.ReadValue()
		if err == io.EOF {
			break
		} else if err != nil {
			return err
		}

		_ = value
	}

	slog.Info("read records2", "took", time.Since(start))

	return nil
}

func main() {
	if err := appMain(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

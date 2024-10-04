package main

import (
	"encoding/json"
	"flag"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/tinyrange/tinyrange/pkg/config"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
)

var outDir = flag.String("outDir", "local/streamableTest", "The path to create the streamable index.")
var configFilename = flag.String("config", "config.json", "The path to the config file to write.")

func appMain() error {
	flag.Parse()

	input := flag.Arg(0)

	contents, err := os.ReadFile(input)
	if err != nil {
		return err
	}

	var config config.TinyRangeConfig

	if err := json.Unmarshal(contents, &config); err != nil {
		return err
	}

	archivePath := filepath.Join(*outDir, "archives")

	if err := os.MkdirAll(archivePath, os.ModePerm); err != nil {
		return err
	}

	for _, frag := range config.RootFsFragments {
		if frag.Archive != nil {
			arkFile := filesystem.NewLocalFile(frag.Archive.HostFilename, nil)

			defName := filepath.Base(frag.Archive.HostFilename)

			arkName := filepath.Join(archivePath, defName)

			out, err := os.Create(arkName)
			if err != nil {
				return err
			}
			defer out.Close()

			writer := filesystem.NewFilesystemStreamableWriter(*outDir)

			if err := filesystem.ExtractArchiveToStreamableIndex(arkFile, out, writer); err != nil {
				return err
			}

			frag.Archive.HostFilename = filepath.Join("archives", defName)
		}
	}

	outputConfig, err := json.Marshal(&config)
	if err != nil {
		return err
	}

	if err := os.WriteFile(filepath.Join(*outDir, *configFilename), outputConfig, os.ModePerm); err != nil {
		return err
	}

	return nil
}

func main() {
	if err := appMain(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

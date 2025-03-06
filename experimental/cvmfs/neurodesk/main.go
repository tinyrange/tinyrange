package main

import (
	"bufio"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/tinyrange/tinyrange/experimental/cvmfs"
	"github.com/tinyrange/tinyrange/pkg/build2"
	"github.com/tinyrange/tinyrange/pkg/builder"
	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/database"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
)

const (
	NEURODESK_MIRROR = "http://cvmfs.neurodesk.org/cvmfs"
	NEURODESK_REPO   = "neurodesk.ardc.edu.au"
	NEURODESK_LOg    = "https://raw.githubusercontent.com/NeuroDesk/neurocommand/main/cvmfs/log.txt"
)

type NeurodeskContainer struct {
	Name       string
	Version    string
	Categories []string
}

func downloadAndParseContainerList(db common.PackageDatabase) ([]NeurodeskContainer, error) {
	logDef := builder.Factory.NewFetchHttpBuildDefinition(NEURODESK_LOg, 2*time.Hour, nil)

	art, err := db.Builder().Build(logDef, common.BuildOptions{})
	if err != nil {
		return nil, err
	}

	logDefFile, err := art.Default()
	if err != nil {
		return nil, err
	}

	logFile, err := logDefFile.Open()
	if err != nil {
		return nil, err
	}

	var containers []NeurodeskContainer

	scanner := bufio.NewScanner(logFile)
	for scanner.Scan() {
		line := scanner.Text()

		tokens := strings.SplitN(line, " ", 2)

		name, version, ok := strings.Cut(tokens[0], "_")
		if !ok {
			return nil, fmt.Errorf("invalid container name: %s", tokens[0])
		}

		categories := strings.Split(strings.TrimPrefix(tokens[1], "categories:"), ",")

		containers = append(containers, NeurodeskContainer{
			Name:       name,
			Version:    version,
			Categories: categories[:len(categories)-1],
		})
	}

	return containers, nil
}

var (
	buildDir         = flag.String("build-dir", "local/cvmfs", "Directory to store build artifacts")
	jobs             = flag.Int("jobs", 1, "Number of jobs to run in parallel")
	mirror           = flag.String("mirror", NEURODESK_MIRROR, "CVMFS mirror to use")
	containerName    = flag.String("name", "", "Name of the container (empty to list)")
	containerVersion = flag.String("version", "", "Version of the container (empty to list)")
)

func appMain() error {
	flag.Parse()

	db, err := database.New(func(pd common.PackageDatabase) (common.Builder, error) {
		if err := common.Ensure(*buildDir, os.ModePerm); err != nil {
			return nil, err
		}

		logger := build2.NewSimpleLogger()

		mutBuildDir := filesystem.NewLocalMutableDirectory(*buildDir)

		buildFs := build2.NewFilesystemBuildCache(mutBuildDir)

		return build2.New(buildFs, pd, *jobs, logger.Group("root")), nil
	})
	if err != nil {
		return err
	}

	containers, err := downloadAndParseContainerList(db)
	if err != nil {
		return err
	}

	if *containerName == "" {
		for _, container := range containers {
			fmt.Printf("%s %+v\n", container.Name, container.Categories)
		}

		return fmt.Errorf("container name is required")
	}

	if *containerVersion == "" {
		for _, container := range containers {
			if container.Name == *containerName {
				fmt.Printf("%s %s\n", container.Name, container.Version)
			}
		}

		return fmt.Errorf("container version is required")
	}

	repo := cvmfs.NewRepository(db, *mirror, NEURODESK_REPO)

	files, err := repo.GetAllFilesWithPrefix(fmt.Sprintf("/containers/%s_%s/%s_%s.simg/", *containerName, *containerVersion, *containerName, *containerVersion))
	if err != nil {
		return err
	}

	_ = files

	// for _, file := range files {
	// 	fmt.Printf("%+v\n", file)
	// }

	return nil
}

func main() {
	if err := appMain(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

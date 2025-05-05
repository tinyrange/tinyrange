package main

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"runtime/pprof"
	"strings"
	"time"

	"github.com/tinyrange/tinyrange/pkg/archive2"
	"github.com/tinyrange/tinyrange/pkg/build2"
	"github.com/tinyrange/tinyrange/pkg/builder"
	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/database"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/filesystem/cvmfs"
	"github.com/tinyrange/tinyrange/pkg/log"
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

type CVMFSArchiveChunk struct {
	Hash   string
	Offset int64
	Size   int64
}

const (
	CVMFS_ARCHIVE_METADATA_KIND = "cvmfs-archive-metadata/v1"
)

type CVMFSArchiveMetadata struct {
	Kind   string
	Mirror string
	Repo   string
	Chunks []CVMFSArchiveChunk
}

var (
	buildDir         = flag.String("build-dir", "local/cvmfs", "Directory to store build artifacts")
	jobs             = flag.Int("jobs", 1, "Number of jobs to run in parallel")
	mirror           = flag.String("mirror", NEURODESK_MIRROR, "CVMFS mirror to use")
	containerName    = flag.String("name", "", "Name of the container (empty to list)")
	containerVersion = flag.String("version", "", "Version of the container (empty to list)")
	outputBase       = flag.String("output-base", "", "Base filename to write a archive2 to")
	cpuProfile       = flag.String("cpu-profile", "", "Write CPU profile to file")
)

func appMain() error {
	flag.Parse()

	if *cpuProfile != "" {
		f, err := os.Create(*cpuProfile)
		if err != nil {
			return err
		}
		defer f.Close()

		if err := pprof.StartCPUProfile(f); err != nil {
			return err
		}
		defer pprof.StopCPUProfile()
	}

	db, err := database.New(func(pd common.PackageDatabase) (common.Builder, error) {
		if err := common.Ensure(*buildDir, os.ModePerm); err != nil {
			return nil, err
		}

		logger := build2.NewSimpleLogger()

		mutBuildDir := filesystem.Factory.NewLocalMutableDirectory(*buildDir)

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

	start := time.Now()

	repo := cvmfs.NewRepository(db.Builder().MinimalContext(), *mirror, NEURODESK_REPO)

	files, err := repo.GetAllFilesWithPrefix(fmt.Sprintf("/containers/%s_%s/%s_%s.simg", *containerName, *containerVersion, *containerName, *containerVersion))
	if err != nil {
		return err
	}

	var arkWriter *archive2.ArchiveWriter

	if *outputBase != "" {
		indexFile, err := os.Create(*outputBase + ".index")
		if err != nil {
			return err
		}
		defer indexFile.Close()

		contentFile, err := os.Create(*outputBase + ".content")
		if err != nil {
			return err
		}
		defer contentFile.Close()

		out, err := archive2.NewArchiveWriter(indexFile, contentFile)
		if err != nil {
			return err
		}
		out.DisablePadding()

		arkWriter = out
	}

	for _, file := range files {
		kind := file.Kind()

		var fac archive2.EntryFactory
		var reader io.Reader

		switch kind {
		case filesystem.TypeDirectory:
			fac = *fac.Kind(archive2.EntryKindDirectory).
				Name(file.FullPath).
				Size(0).
				Mode(fs.FileMode(file.Mode)).
				Owner(int(file.Uid), int(file.Gid)).
				ModTime(time.Unix(file.Mtime, 0))
		case filesystem.TypeSymlink:
			fac = *fac.Kind(archive2.EntryKindSymlink).
				Name(file.FullPath).
				Size(0).
				Mode(fs.FileMode(file.Mode)).
				Owner(int(file.Uid), int(file.Gid)).
				ModTime(time.Unix(file.Mtime, 0)).
				Linkname(file.Symlink)
		case filesystem.TypeRegular:
			var metadata CVMFSArchiveMetadata

			metadata.Kind = CVMFS_ARCHIVE_METADATA_KIND
			metadata.Mirror = *mirror
			metadata.Repo = NEURODESK_REPO

			if file.IsChunked() {
				for _, chunk := range file.Chunks {
					metadata.Chunks = append(metadata.Chunks, CVMFSArchiveChunk{
						Hash:   hex.EncodeToString(chunk.Hash),
						Offset: chunk.Offset,
						Size:   chunk.Size,
					})
				}
			} else {
				metadata.Chunks = append(metadata.Chunks, CVMFSArchiveChunk{
					Hash:   hex.EncodeToString(file.Hash),
					Offset: 0,
					Size:   file.Size,
				})
			}

			metadataMarshaled, err := json.Marshal(metadata)
			if err != nil {
				return fmt.Errorf("failed to marshal metadata: %w", err)
			}

			fac = *fac.Kind(archive2.EntryKindExtended).
				Name(file.FullPath).
				Size(int64(len(metadataMarshaled))).
				Mode(fs.FileMode(file.Mode)).
				Owner(int(file.Uid), int(file.Gid)).
				ModTime(time.Unix(file.Mtime, 0))

			reader = bytes.NewReader(metadataMarshaled)
		default:
			return fmt.Errorf("unknown kind: %s", kind)
		}

		if arkWriter != nil {
			if err := arkWriter.WriteEntry(&fac, reader); err != nil {
				return fmt.Errorf("failed to write entry: %w", err)
			}
		}
	}

	log.Info("done", "duration", time.Since(start))

	return nil
}

func main() {
	if err := appMain(); err != nil {
		log.Error("fatal", "error", err)
		os.Exit(1)
	}
}

package main

import (
	"flag"
	"log/slog"
	"os"

	"github.com/tinyrange/tinyrange/experimental/cvmfs"
	"github.com/tinyrange/tinyrange/pkg/build2"
	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/database"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
)

var (
	mirrorName = flag.String("mirror", "", "CVMFS mirror to use")
	repoName   = flag.String("repo", "", "CVMFS repository to use")
	buildDir   = flag.String("build-dir", "local/cvmfs", "Directory to store build artifacts")
	jobs       = flag.Int("jobs", 1, "Number of jobs to run in parallel")
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

	repo := cvmfs.NewRepository(db, *mirrorName, *repoName)

	var iterateCatalog func(catalog *cvmfs.CVMFSCatalog) error

	catalog, err := repo.RootCatalog()
	if err != nil {
		return err
	}

	iterateCatalog = func(catalog *cvmfs.CVMFSCatalog) error {
		nested, err := catalog.NestedCatalogs()
		if err != nil {
			return err
		}

		for _, nestedCatalog := range nested {
			slog.Info("nested catalog", "path", nestedCatalog.Path, "sha1", nestedCatalog.Sha1, "size", nestedCatalog.Size)

			child, err := repo.GetCatalog(nestedCatalog)
			if err != nil {
				return err
			}

			if err := iterateCatalog(child); err != nil {
				return err
			}
		}

		return nil
	}

	if err := iterateCatalog(catalog); err != nil {
		return err
	}

	return nil
}

func main() {
	if err := appMain(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

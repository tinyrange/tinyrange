package cli

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"runtime/debug"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tinyrange/tinyrange/pkg/build2"
	"github.com/tinyrange/tinyrange/pkg/builder"
	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/database"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/fsutil"
	"github.com/tinyrange/tinyrange/pkg/path"
)

var (
	rootBuildDir          string
	rootBuildCache        []string
	rootRebuild           bool
	rootCpuProfile        string
	rootVerbose           bool
	rootMirrors           []string
	rootBuildJobs         int
	rootExperimentalFlags []string
)

var rootCmd = &cobra.Command{
	Use:   "tinyrange",
	Short: "TinyRange: Next-generation Virtualization for Cyber and beyond",
	Long: `TinyRange version unknown
	Built at The University of Queensland
	Complete documentation is available at https://github.com/tinyrange/tinyrange`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if rootVerbose || os.Getenv("TINYRANGE_VERBOSE") == "on" {
			if err := common.EnableVerbose(); err != nil {
				return err
			}
		}

		if err := common.SetExperimental(rootExperimentalFlags); err != nil {
			return err
		}

		return nil
	},
}

func getBuildDir() (string, error) {
	// Make sure the build dir doesn't have any weird characters in it.
	buildDir, err := path.Native.Abs(rootBuildDir)
	if err != nil {
		return "", err
	}

	return buildDir, nil
}

func parseCacheToDirectory(db common.PackageDatabase, cache string) (filesystem.Directory, filesystem.BuildDatabaseConfig, error) {
	url, err := url.Parse(cache)
	if err != nil {
		return nil, filesystem.BuildDatabaseConfig{}, fmt.Errorf("failed to parse cache URL: %w", err)
	}

	if url.Scheme == "file" {
		absPath, err := path.Native.Abs(url.Host + url.Path)
		if err != nil {
			return nil, filesystem.BuildDatabaseConfig{}, fmt.Errorf("failed to get absolute path: %w", err)
		}

		stat, err := os.Stat(absPath)
		if err != nil {
			return nil, filesystem.BuildDatabaseConfig{}, fmt.Errorf("failed to stat cache directory: %w", err)
		}

		if stat.IsDir() {
			dir := filesystem.NewLocalDirectory(absPath)

			return dir, filesystem.BuildDatabaseConfig{
				AbsoluteHostBuildDirectory: &filesystem.AbsoluteHostBuildDirectory{
					AbsolutePath: absPath,
				},
			}, nil
		} else {
			// Assume it's an archive.

			kind, ok := builder.ReadArchiveSupportsExtracting(absPath, true)
			if ok {
				hash, err := common.Sha256HashFromFile(absPath)
				if err != nil {
					return nil, filesystem.BuildDatabaseConfig{}, err
				}

				def := builder.Factory.NewConstantHashDefinition(hash, func() (io.ReadCloser, error) {
					return os.Open(absPath)
				})
				ark := builder.Factory.NewReadArchive2BuildDefinition(def, kind, 0)

				art, err := db.Builder().Build(ark, common.BuildOptions{})
				if err != nil {
					return nil, filesystem.BuildDatabaseConfig{}, err
				}

				archive, err := builder.Archive2FromArtifact(art)
				if err != nil {
					return nil, filesystem.BuildDatabaseConfig{}, err
				}

				var buildDirTop filesystem.Directory
				top := filesystem.NewMemoryDirectory()

				if err := fsutil.ExtractArchive2ToFilesystem(archive, "", top); err != nil {
					return nil, filesystem.BuildDatabaseConfig{}, fmt.Errorf("failed to extract archive: %w", err)
				}

				pathString := url.Query().Get("path")

				if pathString != "" {
					topEnt, err := filesystem.OpenPath(top, pathString)
					if err != nil {
						return nil, filesystem.BuildDatabaseConfig{}, fmt.Errorf("failed to open path in archive: %w", err)
					}

					dir, ok := topEnt.File.(filesystem.Directory)
					if !ok {
						return nil, filesystem.BuildDatabaseConfig{}, fmt.Errorf("path in archive is not a directory: %s", pathString)
					}

					buildDirTop = dir
				} else {
					buildDirTop = top
				}

				return buildDirTop, filesystem.BuildDatabaseConfig{
					Archive2BuildArtifact: &filesystem.Archive2BuildArtifact{
						Hash: art.DefinitionHash().String(),
						Path: pathString,
					},
				}, nil
			} else {
				return nil, filesystem.BuildDatabaseConfig{}, fmt.Errorf("cache is not a directory or archive: %s", absPath)
			}
		}
	} else {
		return nil, filesystem.BuildDatabaseConfig{}, fmt.Errorf("unsupported cache type: %s", url.Scheme)
	}
}

func newDb() (common.PackageDatabase, error) {
	buildDir, err := getBuildDir()
	if err != nil {
		return nil, err
	}

	builderFactory := func(db common.PackageDatabase) (common.Builder, error) {
		logger := build2.NewSimpleLogger()

		// Check with Exists first so it doesn't have issues if the build dir is behind a symlink.
		ok, err := common.Exists(buildDir)
		if err != nil {
			return nil, err
		}

		if !ok {
			if err := common.Ensure(buildDir, os.ModePerm); err != nil {
				return nil, err
			}
		}

		buildDirMut := filesystem.NewLocalMutableDirectory(buildDir)

		buildFs := build2.NewFilesystemBuildCache(buildDirMut, build2.DEFAULT_DATABASE_CONFIG)

		return build2.New(buildFs, db, rootBuildJobs, logger.Group("builder")), nil
	}

	db, err := database.New(builderFactory)
	if err != nil {
		return nil, err
	}

	buildFs := db.Builder().Filesystem().(build2.BuildCacheFilesystem)

	for _, cache := range rootBuildCache {
		cacheDir, cfg, err := parseCacheToDirectory(db, cache)
		if err != nil {
			return nil, err
		}

		if err := buildFs.AddCacheDirectory(cacheDir, cfg); err != nil {
			return nil, err
		}
	}

	db.Builder().SetRebuildUserDefinitions(rootRebuild)

	for _, mirror := range rootMirrors {
		name, url, ok := strings.Cut(mirror, "=")
		if !ok {
			return nil, fmt.Errorf("invalid mirror syntax (name=url)")
		}

		if err := db.AddMirror(name, []string{url}); err != nil {
			return nil, err
		}
	}

	return db, nil
}

func init() {
	buildinfo, ok := debug.ReadBuildInfo()
	if ok {
		rootCmd.Long = fmt.Sprintf(`TinyRange version %s
Built at The University of Queensland
Complete documentation is available at https://github.com/tinyrange/tinyrange`, buildinfo.Main.Version)
	}

	rootCmd.PersistentFlags().StringVar(&rootBuildDir, "buildDir", common.GetDefaultBuildDir(), "specify the directory for built definitions and temporary files")
	rootCmd.PersistentFlags().StringArrayVar(&rootBuildCache, "buildCache", []string{}, "specify a series of read-only directories to use as build caches, format file://<path>")
	rootCmd.PersistentFlags().BoolVar(&rootRebuild, "rebuild", false, "should user package definitions be rebuilt even if we already have built them previously")
	rootCmd.PersistentFlags().StringVar(&rootCpuProfile, "cpuprofile", "", "write cpu profile to file")
	rootCmd.PersistentFlags().BoolVar(&rootVerbose, "verbose", false, "enable debugging output")
	rootCmd.PersistentFlags().StringArrayVar(&rootMirrors, "mirror", []string{}, "Specify mirrors to override the default mirror settings")
	rootCmd.PersistentFlags().IntVar(&rootBuildJobs, "jobs", 1, "specify the number of jobs to run concurrently")
	rootCmd.PersistentFlags().StringArrayVar(&rootExperimentalFlags, "experimental", []string{}, "Add experimental flags.")
}

func Run() {
	// check if we are passing a single argument.
	if len(os.Args) == 2 {
		// If the argument ends with .yaml then assume it's a config file.
		if strings.HasSuffix(os.Args[1], ".yaml") {
			runConfig(os.Args[1])
			return
		}
	}

	if err := rootCmd.Execute(); err != nil {
		// fmt.Println(err)
		os.Exit(1)
	}
}

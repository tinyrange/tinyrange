package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tinyrange/tinyrange/pkg/build1"
	"github.com/tinyrange/tinyrange/pkg/build2"
	"github.com/tinyrange/tinyrange/pkg/buildinfo"
	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/database"
	"github.com/tinyrange/tinyrange/pkg/feature"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/path"
)

var (
	rootBuildDir          string
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
	Long: fmt.Sprintf(`TinyRange version %s
Built at The University of Queensland
Complete documentation is available at https://github.com/tinyrange/tinyrange`, buildinfo.VERSION),
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if rootVerbose || os.Getenv("TINYRANGE_VERBOSE") == "on" {
			if err := common.EnableVerbose(); err != nil {
				return err
			}
		}

		if len(rootExperimentalFlags) > 0 {
			if err := common.SetExperimental(rootExperimentalFlags); err != nil {
				return err
			}
		}

		return nil
	},
}

func newDb() (common.PackageDatabase, error) {
	builderFactory := func(db common.PackageDatabase) (common.Builder, error) {
		logger := build2.NewSimpleLogger()

		// Make sure the build dir doesn't have any weird characters in it.
		buildDir, err := path.Native.Abs(rootBuildDir)
		if err != nil {
			return nil, err
		}

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

		return build2.New(buildDirMut, db, rootBuildJobs, logger.Group("builder")), nil
	}

	if feature.HasFeature(feature.FeatureBuild1) {
		builderFactory = build1.NewBuilder(rootBuildDir)
	}

	db, err := database.New(builderFactory)
	if err != nil {
		return nil, err
	}

	db.Builder().SetRebuildUserDefinitions(rootRebuild)

	for _, mirror := range rootMirrors {
		name, url, ok := strings.Cut(mirror, "=")
		if !ok {
			return nil, fmt.Errorf("invalid mirror syntax (name=url)")
		}

		db.AddMirror(name, []string{url})
	}

	return db, nil
}

func init() {
	rootCmd.PersistentFlags().StringVar(&rootBuildDir, "buildDir", common.GetDefaultBuildDir(), "specify the directory for built definitions and temporary files")
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

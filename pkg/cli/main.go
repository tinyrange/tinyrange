package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tinyrange/tinyrange/pkg/buildinfo"
	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/database"
)

var (
	rootBuildDir     string
	rootRebuild      bool
	rootCpuProfile   string
	rootVerbose      bool
	rootDistribution string
	rootMirrors      []string
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

		return nil
	},
}

func newDb() (*database.PackageDatabase, error) {
	db := database.New(rootBuildDir)

	if rootDistribution != "" {
		if err := db.SetDistributionServer(rootDistribution); err != nil {
			return nil, err
		}
	}

	// Check with Exists first so it doesn't have issues if the build dir is behind a symlink.
	if ok, _ := common.Exists(rootBuildDir); !ok {
		if err := common.Ensure(rootBuildDir, os.ModePerm); err != nil {
			return nil, err
		}
	}

	db.RebuildUserDefinitions = rootRebuild

	if err := db.LoadBuiltinBuilders(); err != nil {
		return nil, err
	}

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
	rootCmd.PersistentFlags().StringVar(&rootDistribution, "distribution", "", "The HTTP/HTTPS address of a distribution server to copy build results from")
	rootCmd.PersistentFlags().StringArrayVar(&rootMirrors, "mirror", []string{}, "Specify mirrors to override the default mirror settings")
}

func Run() {
	if err := rootCmd.Execute(); err != nil {
		// fmt.Println(err)
		os.Exit(1)
	}
}

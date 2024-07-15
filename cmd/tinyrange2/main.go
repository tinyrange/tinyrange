package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/tinyrange/tinyrange/pkg/buildinfo"
	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/database"
)

var rootBuildDir string
var rootRebuild bool

var rootCmd = &cobra.Command{
	Use:   "tinyrange",
	Short: "TinyRange: Next-generation Virtualization for Cyber and beyond",
	Long: fmt.Sprintf(`TinyRange version %s
Built at The University of Queensland
Complete documentation is available at https://github.com/tinyrange/tinyrange`, buildinfo.VERSION),
}

func newDb() (*database.PackageDatabase, error) {
	db := database.New(rootBuildDir)

	db.RebuildUserDefinitions = rootRebuild

	if err := db.LoadBuiltinBuilders(); err != nil {
		return nil, err
	}

	return db, nil
}

func init() {
	rootCmd.PersistentFlags().StringVar(&rootBuildDir, "buildDir", common.GetDefaultBuildDir(), "specify the directory for built definitions and temporary files")
	rootCmd.PersistentFlags().BoolVar(&rootRebuild, "rebuild", false, "should user package definitions be rebuilt even if we already have built them previously")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		// fmt.Println(err)
		os.Exit(1)
	}
}

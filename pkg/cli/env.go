package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var envCmd = &cobra.Command{
	Use:   "env",
	Short: "Manage the TinyRange environment",
}

var buildDirCmd = &cobra.Command{
	Use:   "build-dir",
	Short: "Print the build directory",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Fprintf(os.Stdout, "%s", rootBuildDir)

		return nil
	},
}

func init() {
	rootCmd.AddCommand(envCmd)

	envCmd.AddCommand(buildDirCmd)
}

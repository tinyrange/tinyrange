package main

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tinyrange/tinyrange/pkg/common"
)

var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "Build a single definiton",
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return fmt.Errorf("please specify a definition")
		}

		db, err := newDb()
		if err != nil {
			return err
		}

		def, err := db.GetDefinitionByHash(args[0])
		if err != nil {
			return err
		}

		if _, err := db.Build(db.NewBuildContext(def), def, common.BuildOptions{
			AlwaysRebuild: true,
		}); err != nil {
			return err
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(buildCmd)
}

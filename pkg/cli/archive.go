package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/tinyrange/tinyrange/pkg/archive"
	"github.com/tinyrange/tinyrange/pkg/hash"
	"github.com/tinyrange/tinyrange/pkg/log"
)

var archiveCmd = &cobra.Command{
	Use:   "archive",
	Short: "Manage archived build artifacts",
}

var archiveListCmd = &cobra.Command{
	Use:   "list <hash> <name>",
	Short: "List entries in an archived build artifact",
	RunE: func(cmd *cobra.Command, args []string) error {
		db, err := newDb()
		if err != nil {
			return err
		}

		if len(args) != 2 {
			return fmt.Errorf("please specify a hash and name")
		}

		name := args[1]

		art, err := db.Builder().Filesystem().GetBuildDirectory(hash.Hash(args[0]))
		if err != nil {
			return fmt.Errorf("failed to get build artifact: %w", err)
		}

		out, err := art.File(name)
		if err != nil {
			return fmt.Errorf("failed to get file from artifact: %w", err)
		}

		ark, err := archive.ReadArchiveFromFile(out)
		if err != nil {
			return fmt.Errorf("failed to read archive: %w", err)
		}
		ents, err := ark.Entries()
		if err != nil {
			return fmt.Errorf("failed to list archive entries: %w", err)
		}

		log.Default().Info("", "count", len(ents))

		for _, e := range ents {
			fmt.Printf("%s\n", e.Name())
		}

		return nil
	},
}

func init() {
	archiveCmd.AddCommand(archiveListCmd)

	rootCmd.AddCommand(archiveCmd)
}

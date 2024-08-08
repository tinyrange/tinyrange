package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var storeCmd = &cobra.Command{
	Use:   "store",
	Short: "List all definitions in the store",
	RunE: func(cmd *cobra.Command, args []string) error {
		db, err := newDb()
		if err != nil {
			return err
		}

		hashes, err := db.GetAllHashes()
		if err != nil {
			return err
		}

		for _, hash := range hashes {
			def, err := db.GetDefinitionByHash(hash)
			if err != nil {
				return fmt.Errorf("failed to get definition for hash %s: %s", hash, err)
			}

			fmt.Printf("%s - %s\n", hash, def.Tag())
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(storeCmd)
}

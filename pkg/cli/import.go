package cli

import (
	"encoding/json"
	"log/slog"
	"os"

	"github.com/spf13/cobra"
)

type importData struct {
	Definitions []json.RawMessage
}

var importCmd = &cobra.Command{
	Use:   "import",
	Short: "Import a series of definitions but do not build them",
	RunE: func(cmd *cobra.Command, args []string) error {
		db, err := newDb()
		if err != nil {
			return err
		}

		var data importData
		if err := json.NewDecoder(os.Stdin).Decode(&data); err != nil {
			return err
		}

		for _, def := range data.Definitions {
			buildDef, err := db.Builder().ImportAndValidate(def)
			if err != nil {
				return err
			}

			slog.Info("imported", "definition", buildDef)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(importCmd)
}

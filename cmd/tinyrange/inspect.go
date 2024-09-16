package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/tinyrange/tinyrange/pkg/common"
)

var (
	inspectRaw bool
)

var inspectCmd = &cobra.Command{
	Use:   "inspect",
	Short: "Inspect a single definition or a raw file",
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return fmt.Errorf("please specify a definition")
		}

		if inspectRaw {
			return fmt.Errorf("--raw not implemented")
		}

		db, err := newDb()
		if err != nil {
			return err
		}

		ctx := db.NewMacroContext()

		macro, err := db.GetMacroByShorthand(ctx, args[0])
		if err != nil {
			return err
		}

		ret, err := macro.Call(ctx)
		if err != nil {
			return err
		}

		if def, ok := ret.(common.BuildDefinition); ok {
			if err := db.Inspect(def, os.Stdout); err != nil {
				return err
			}
			return nil
		} else {
			return fmt.Errorf("could not convert %T to BuildDefinition", ret)
		}
	},
}

func init() {
	inspectCmd.PersistentFlags().BoolVarP(&inspectRaw, "raw", "r", false, "if specified then ")
	rootCmd.AddCommand(inspectCmd)
}

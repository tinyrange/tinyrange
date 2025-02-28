package cli

import (
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/spf13/cobra"
	"github.com/tinyrange/tinyrange/pkg/common"
)

var (
	buildOutput   string
	buildUseCache bool
)

var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "Build a single definition",
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return fmt.Errorf("please specify a definition")
		}

		db, err := newDb()
		if err != nil {
			return err
		}

		macroCtx := db.NewMacroContext()

		macro, err := db.GetMacroByShorthand(macroCtx, args[0], true)
		if err != nil {
			return err
		}

		ret, err := macro.Call(macroCtx)
		if err != nil {
			return err
		}

		if ret == nil {
			return nil
		}

		if def, ok := ret.(common.BuildDefinition); ok {
			art, err := db.Builder().Build(def, common.BuildOptions{
				AlwaysRebuild: !buildUseCache,
			})
			if err != nil {
				slog.Error("fatal", "err", err)
				os.Exit(1)
			}

			f, err := art.Default()
			if err != nil {
				return err
			}

			if buildOutput != "" {
				fh, err := f.Open()
				if err != nil {
					return err
				}
				defer fh.Close()

				out, err := os.Create(buildOutput)
				if err != nil {
					return err
				}
				defer out.Close()

				if _, err := io.Copy(out, fh); err != nil {
					return err
				}
			}

			return nil
		} else {
			return fmt.Errorf("could not convert %T to BuildDefinition", ret)
		}
	},
}

func init() {
	buildCmd.PersistentFlags().StringVarP(&buildOutput, "output", "o", "", "if specified then copy the build output to a local file at path")
	buildCmd.PersistentFlags().BoolVarP(&buildUseCache, "use-cache", "c", false, "if specified then don't rebuild the top level definition if it already exists in the cache")
	rootCmd.AddCommand(buildCmd)
}

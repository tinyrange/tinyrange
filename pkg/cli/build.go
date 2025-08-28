package cli

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/tinyrange/tinyrange/pkg/common"
)

var (
	buildOutput        string
	buildUseCache      bool
	buildGetVMTemplate bool
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
			if buildGetVMTemplate {
				vmDef, ok := def.(common.BuildVmDefinition)
				if !ok {
					return fmt.Errorf("definition is not a VM definition")
				}

				vmDef.SetBuildTemplateMode()

				_, err := db.Builder().Build(vmDef, common.BuildOptions{AlwaysRebuild: true})
				var built common.ErrTemplateBuilt
				if errors.As(err, &built) {
					fmt.Printf("%s\n", string(built))

					return nil
				} else if err != nil {
					return err
				} else {
					return fmt.Errorf("failed to write template output")
				}
			}

			art, err := db.Builder().Build(def, common.BuildOptions{
				AlwaysRebuild: !buildUseCache,
			})
			if err != nil {
				db.Logger().Error("fatal", "err", err)
				os.Exit(1)
			}

			if buildOutput != "" {
				f, err := art.Default()
				if err != nil {
					return err
				}

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
	buildCmd.PersistentFlags().BoolVar(&buildGetVMTemplate, "get-vm-template", false, "if specified then get the VM template instead of running the VM")
	rootCmd.AddCommand(buildCmd)
}

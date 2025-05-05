package cli

import (
	"fmt"
	"io"
	"os"
	"runtime/pprof"

	"github.com/spf13/cobra"
	"github.com/tinyrange/tinyrange/pkg/builder"
	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/log"
	"github.com/tinyrange/tinyrange/pkg/path"
)

var (
	scriptFiles  []string
	scriptOutput string
)

var scriptCmd = &cobra.Command{
	Use:   "script",
	Short: "Run a starlark script",
	RunE: func(cmd *cobra.Command, args []string) error {
		if rootCpuProfile != "" {
			f, err := os.Create(rootCpuProfile)
			if err != nil {
				return err
			}
			pprof.StartCPUProfile(f)
			defer pprof.StopCPUProfile()
		}

		db, err := newDb()
		if err != nil {
			return err
		}

		if len(args) == 0 {
			return fmt.Errorf("no filename specified")
		}

		files := make(map[string]filesystem.File)

		for _, file := range scriptFiles {
			hash, err := common.Sha256HashFromFile(file)
			if err != nil {
				return err
			}

			def := builder.Factory.NewConstantHashDefinition(hash, func() (io.ReadCloser, error) {
				return os.Open(file)
			})

			f := filesystem.Factory.NewLocalFile(file, def)
			files[path.Native.Base(file)] = f
		}

		filename := args[0]

		if err := db.RunScript(filename, files, args[1:], scriptOutput); err != nil {
			log.Error("fatal", "err", err)
			os.Exit(1)
		}

		return nil
	},
}

func init() {
	scriptCmd.PersistentFlags().StringArrayVar(&scriptFiles, "files", []string{}, "pass a list of files to the script.")
	scriptCmd.PersistentFlags().StringVarP(&scriptOutput, "output", "O", "", "copy the build result to this file.")
	rootCmd.AddCommand(scriptCmd)
}

package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime/pprof"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tinyrange/tinyrange/pkg/config"
	"github.com/tinyrange/tinyrange/pkg/tinyrange"
	"gopkg.in/yaml.v3"
)

var (
	runDebug            bool
	runExportFilesystem string
	runListenNbd        string
)

var runCmd = &cobra.Command{
	Use:   "run <config>",
	Short: "Run a virtual machine from a configuration file",
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return fmt.Errorf("run requires a configuration file")
		}

		if rootCpuProfile != "" {
			f, err := os.Create(rootCpuProfile)
			if err != nil {
				return err
			}
			pprof.StartCPUProfile(f)
			defer pprof.StopCPUProfile()
		}

		f, err := os.Open(args[0])
		if err != nil {
			return err
		}
		defer f.Close()

		var cfg config.TinyRangeConfig

		if strings.HasSuffix(f.Name(), ".json") {

			dec := json.NewDecoder(f)

			if err := dec.Decode(&cfg); err != nil {
				return err
			}
		} else if strings.HasSuffix(f.Name(), ".yml") {
			dec := yaml.NewDecoder(f)

			if err := dec.Decode(&cfg); err != nil {
				return err
			}
		}

		return tinyrange.RunWithConfig(rootBuildDir, cfg, runDebug, false, runExportFilesystem, runListenNbd)
	},
}

func init() {
	runCmd.PersistentFlags().BoolVar(&runDebug, "debug", false, "redirect output from the hypervisor to the host. the guest will exit as soon as the VM finishes startup")
	runCmd.PersistentFlags().StringVar(&runExportFilesystem, "export-filesystem", "", "write the filesystem to the host filesystem")
	runCmd.PersistentFlags().StringVar(&runListenNbd, "listen-nbd", "", "Listen with an NBD server on the given address and port")
	rootCmd.AddCommand(runCmd)
}

package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"
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
	runStreamingServer  string
	runWireguardUrl     string
	runSecureSSH        string
)

var runCmd = &cobra.Command{
	Use:   "run-vm <config>",
	Short: "Run a virtual machine from a configuration file",
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 && runStreamingServer == "" {
			return fmt.Errorf("run-vm requires a configuration file")
		}

		if rootCpuProfile != "" {
			f, err := os.Create(rootCpuProfile)
			if err != nil {
				return err
			}
			pprof.StartCPUProfile(f)
			defer pprof.StopCPUProfile()
		}

		var configs []config.TinyRangeConfig

		if runStreamingServer != "" {
			resp, err := http.Get(runStreamingServer)
			if err != nil {
				return err
			}
			defer resp.Body.Close()

			var cfg config.TinyRangeConfig

			dec := json.NewDecoder(resp.Body)

			if err := dec.Decode(&cfg); err != nil {
				return err
			}

			url, err := url.Parse(runStreamingServer)
			if err != nil {
				return err
			}

			url.Path = path.Dir(url.Path)

			runStreamingServer = url.String()

			configs = append(configs, cfg)
		} else {
			for _, configFilename := range args {
				f, err := os.Open(configFilename)
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

				configs = append(configs, cfg)
			}
		}

		return tinyrange.RunWithConfig(rootBuildDir, configs, runDebug, false, runExportFilesystem, runListenNbd, runStreamingServer, runWireguardUrl, runSecureSSH)
	},
}

func init() {
	runCmd.PersistentFlags().BoolVar(&runDebug, "debug", false, "redirect output from the hypervisor to the host. the guest will exit as soon as the VM finishes startup")
	runCmd.PersistentFlags().StringVar(&runExportFilesystem, "export-filesystem", "", "write the filesystem to the host filesystem")
	runCmd.PersistentFlags().StringVar(&runListenNbd, "listen-nbd", "", "Listen with an NBD server on the given address and port")
	runCmd.PersistentFlags().StringVar(&runStreamingServer, "stream", "", "Specify a server to download the config from.")
	runCmd.PersistentFlags().StringVar(&runWireguardUrl, "wireguard", "", "Specify a WireGuard server to download a config from.")
	runCmd.PersistentFlags().StringVar(&runSecureSSH, "secure-ssh", "", "Specify a local file to save a secure SSH config to. This will set a random persistent host key and root password.")
	rootCmd.AddCommand(runCmd)
}

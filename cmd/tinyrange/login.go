package cli

import (
	"os"
	"path/filepath"
	"runtime/pprof"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/tinyrange/tinyrange/pkg/builder"
	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/login"
	"gopkg.in/yaml.v3"
)

const DEFAuLT_BUILDER = "alpine@3.20"

var currentConfig login.Config = login.Config{Version: login.CURRENT_CONFIG_VERSION}

var (
	loginSaveConfig string
	loginLoadConfig string
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Start a virtual machine with a builder and a list of packages",
	RunE: func(cmd *cobra.Command, args []string) error {
		if rootCpuProfile != "" {
			f, err := os.Create(rootCpuProfile)
			if err != nil {
				return err
			}
			pprof.StartCPUProfile(f)
			defer pprof.StopCPUProfile()
		}

		if len(currentConfig.ExperimentalFlags) > 0 {
			if err := common.SetExperimental(currentConfig.ExperimentalFlags); err != nil {
				return err
			}
		}

		currentConfig.Packages = args

		if loginSaveConfig != "" {
			cfg, err := yaml.Marshal(&currentConfig)
			if err != nil {
				return err
			}

			return os.WriteFile(loginSaveConfig, cfg, os.FileMode(0644))
		} else {
			db, err := newDb()
			if err != nil {
				return err
			}

			if loginLoadConfig != "" {
				var addedCommands []string
				var additionalPorts []string

				// add the commands from the command line
				addedCommands = append(addedCommands, currentConfig.Commands...)
				additionalPorts = append(additionalPorts, currentConfig.ForwardPorts...)

				// check if loginLoadConfig is a URL
				if strings.HasPrefix(loginLoadConfig, "http://") || strings.HasPrefix(loginLoadConfig, "https://") {
					// expire after 1 hour
					def := builder.NewFetchHttpBuildDefinition(loginLoadConfig, 1*time.Hour, nil)

					f, err := db.Build(db.NewBuildContext(def), def, common.BuildOptions{})
					if err != nil {
						return err
					}

					fh, err := f.Open()
					if err != nil {
						return err
					}
					defer fh.Close()

					if err := yaml.NewDecoder(fh).Decode(&currentConfig); err != nil {
						return err
					}
				} else {
					f, err := os.Open(loginLoadConfig)
					if err != nil {
						return err
					}
					defer f.Close()

					dec := yaml.NewDecoder(f)

					if err := dec.Decode(&currentConfig); err != nil {
						return err
					}

					currentConfig.SetLocalConfig()
				}

				currentConfig.SetBasePath(filepath.Dir(loginLoadConfig))

				if len(addedCommands) > 0 {
					if len(currentConfig.Commands) > 0 {
						// Remove the last command from the end of the config (normally a shell or a entrypoint)
						currentConfig.Commands = currentConfig.Commands[:len(currentConfig.Commands)-1]

						// Add the new commands
						currentConfig.Commands = append(currentConfig.Commands, addedCommands...)
					} else {
						currentConfig.Commands = addedCommands
					}
				}

				if len(additionalPorts) > 0 {
					currentConfig.ForwardPorts = append(currentConfig.ForwardPorts, additionalPorts...)
				}
			} else {
				currentConfig.SetLocalConfig()

				wd, err := os.Getwd()
				if err != nil {
					return err
				}
				currentConfig.SetBasePath(wd)
			}

			return currentConfig.Run(db)
		}
	},
}

func init() {
	// config flags
	loginCmd.PersistentFlags().StringVarP(&loginSaveConfig, "save-config", "w", "", "Write the config to a given file and don't run it.")
	loginCmd.PersistentFlags().StringVarP(&loginLoadConfig, "load-config", "c", "", "Load the config from a file and run it.")

	// public flags (saved to config)
	loginCmd.PersistentFlags().StringVarP(&currentConfig.Builder, "builder", "b", DEFAuLT_BUILDER, "The container builder used to construct the virtual machine.")
	loginCmd.PersistentFlags().StringArrayVarP(&currentConfig.Commands, "exec", "E", []string{}, "Run a different command rather than dropping into a shell.")
	loginCmd.PersistentFlags().StringVar(&currentConfig.Init, "init", "", "Replace the init system with a different command.")
	loginCmd.PersistentFlags().BoolVar(&currentConfig.NoScripts, "no-scripts", false, "Disable script execution.")
	loginCmd.PersistentFlags().StringArrayVarP(&currentConfig.Files, "file", "f", []string{}, "Specify local files/URLs to be copied into the virtual machine. URLs will be downloaded to the build directory first.")
	loginCmd.PersistentFlags().StringArrayVarP(&currentConfig.Archives, "archive", "a", []string{}, "Specify archives to be copied into the virtual machine. A copy will be made in the build directory.")
	loginCmd.PersistentFlags().StringVarP(&currentConfig.Output, "output", "o", "", "Write the specified file from the guest to the host.")
	loginCmd.PersistentFlags().StringArrayVarP(&currentConfig.Environment, "environment", "e", []string{}, "Add environment variables to the VM.")
	loginCmd.PersistentFlags().StringArrayVarP(&currentConfig.Macros, "macro", "m", []string{}, "Add macros to the VM.")
	loginCmd.PersistentFlags().StringVar(&currentConfig.Architecture, "arch", "", "Override the CPU architecture of the machine. This will use emulation with a performance hit.")
	loginCmd.PersistentFlags().StringArrayVar(&currentConfig.ForwardPorts, "forward", []string{}, "Forward a port from the guest to the host.")
	loginCmd.PersistentFlags().StringVar(&currentConfig.OciImage, "oci", "", "Use an OCI image as the root filesystem.")

	// private flags (need to set on command line)
	loginCmd.PersistentFlags().IntVar(&currentConfig.CpuCores, "cpu", 1, "The number of CPU cores to allocate to the virtual machine.")
	loginCmd.PersistentFlags().IntVar(&currentConfig.MemorySize, "ram", 1024, "The amount of ram in the virtual machine in megabytes.")
	loginCmd.PersistentFlags().IntVar(&currentConfig.StorageSize, "storage", 1024, "The amount of storage to allocate in the virtual machine in megabytes.")
	loginCmd.PersistentFlags().BoolVar(&currentConfig.Debug, "debug", false, "Redirect output from the hypervisor to the host. the guest will exit as soon as the VM finishes startup.")
	loginCmd.PersistentFlags().StringVar(&currentConfig.WriteRoot, "write-root", "", "Write the root filesystem as a .tar.gz archive.")
	loginCmd.PersistentFlags().StringVar(&currentConfig.WriteDocker, "write-docker", "", "Write the root filesystem to a docker tag on the local docker daemon.")
	loginCmd.PersistentFlags().BoolVar(&currentConfig.Hash, "hash", false, "print the hash of the definition generated after the machine has exited.")
	loginCmd.PersistentFlags().StringArrayVar(&currentConfig.ExperimentalFlags, "experimental", []string{}, "Add experimental flags.")
	loginCmd.PersistentFlags().StringVar(&currentConfig.WebSSH, "web", "", "Start a web interface on the given port.")
	loginCmd.PersistentFlags().BoolVar(&currentConfig.WriteTemplate, "template", false, "If true then just generate the config and don't run the VM.")
	loginCmd.PersistentFlags().StringArrayVar(&currentConfig.ReadOnlyMounts, "mount", []string{}, "Mount a host directory into the VM using SFTP.")
	loginCmd.PersistentFlags().StringArrayVar(&currentConfig.ReadWriteMounts, "mount-rw", []string{}, "Mount a host directory into the VM using SFTP with read-write access.")
	// loginCmd.PersistentFlags().BoolVar(&currentConfig.NoNetwork, "no-network", false, "Disable network access.")
	rootCmd.AddCommand(loginCmd)
}

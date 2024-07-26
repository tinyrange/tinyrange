package main

import (
	"fmt"
	"log/slog"
	"os"
	"runtime/pprof"

	"github.com/spf13/cobra"
	"github.com/tinyrange/tinyrange/pkg/builder"
	"github.com/tinyrange/tinyrange/pkg/common"
)

var (
	loginBuilder     string
	loginCpuCores    int
	loginMemorySize  int
	loginStorageSize int
	loginExec        string
	loginLoadConfig  string
	loginSaveConfig  string
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

		db, err := newDb()
		if err != nil {
			return err
		}

		if loginBuilder == "list" {
			for name, builder := range db.ContainerBuilders {
				fmt.Printf(" - %s - %s\n", name, builder.DisplayName)
			}
			return nil
		}

		if loginLoadConfig != "" {
			f, err := os.Open(loginLoadConfig)
			if err != nil {
				return err
			}
			defer f.Close()

			def, err := builder.UnmarshalDefinition(f)
			if err != nil {
				slog.Error("fatal", "err", err)
				os.Exit(1)
			}

			if _, err := db.Build(db.NewBuildContext(def), def, common.BuildOptions{
				AlwaysRebuild: true,
			}); err != nil {
				slog.Error("fatal", "err", err)
				os.Exit(1)
			}

			return nil
		} else {
			var pkgs []common.PackageQuery

			for _, arg := range args {
				q, err := common.ParsePackageQuery(arg)
				if err != nil {
					return err
				}

				pkgs = append(pkgs, q)
			}

			var dir []common.Directive

			if loginBuilder == "" {
				return fmt.Errorf("please specify a builder")
			}

			planDirective := builder.NewPlanDefinition(loginBuilder, pkgs, common.TagList{"level3"})

			dir = append(dir, planDirective)

			if loginExec != "" {
				dir = append(dir, common.DirectiveRunCommand{Command: loginExec})
			} else {
				dir = append(dir, common.DirectiveRunCommand{Command: "interactive"})
			}

			def := builder.NewBuildVmDefinition(
				dir,
				nil, nil,
				"",
				loginCpuCores, loginMemorySize, loginStorageSize,
				"ssh",
			)

			if loginSaveConfig != "" {
				w, err := os.Create(loginSaveConfig)
				if err != nil {
					return err
				}
				defer w.Close()

				if err := builder.MarshalDefinition(w, def); err != nil {
					return err
				}

				return nil
			} else {
				if _, err := db.Build(db.NewBuildContext(def), def, common.BuildOptions{
					AlwaysRebuild: true,
				}); err != nil {
					slog.Error("fatal", "err", err)
					os.Exit(1)
				}

				return nil
			}
		}
	},
}

func init() {
	loginCmd.PersistentFlags().StringVarP(&loginBuilder, "builder", "b", "", "the container builder used to construct the virtual machine")
	loginCmd.PersistentFlags().IntVar(&loginCpuCores, "cpu", 1, "the number of CPU cores to allocate to the virtual machine")
	loginCmd.PersistentFlags().IntVar(&loginMemorySize, "ram", 1024, "the amount of ram in the virtual machine in megabytes")
	loginCmd.PersistentFlags().IntVar(&loginStorageSize, "storage", 1024, "the amount of storage to allocate in the virtual machine in megabytes")
	loginCmd.PersistentFlags().StringVarP(&loginExec, "exec", "E", "", "run a different command rather than dropping into a shell")
	loginCmd.PersistentFlags().StringVar(&loginSaveConfig, "save-definition", "", "serialize the definition to the specified filename")
	loginCmd.PersistentFlags().StringVarP(&loginLoadConfig, "load-definition", "c", "", "run a virtual machine from a serialized definition")
	rootCmd.AddCommand(loginCmd)
}

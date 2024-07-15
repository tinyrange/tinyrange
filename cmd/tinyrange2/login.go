package main

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tinyrange/tinyrange/pkg/builder"
	"github.com/tinyrange/tinyrange/pkg/common"
)

var loginBuilder string
var loginCpuCores int
var loginMemorySize int
var loginStorageSize int

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Start a virtual machine with a builder and a list of packages",
	RunE: func(cmd *cobra.Command, args []string) error {
		if loginBuilder == "" {
			return fmt.Errorf("please specify a builder")
		}

		db, err := newDb()
		if err != nil {
			return err
		}

		var pkgs []common.PackageQuery

		for _, arg := range args {
			q, err := common.ParsePackageQuery(arg)
			if err != nil {
				return err
			}

			pkgs = append(pkgs, q)
		}

		def := builder.NewBuildVmDefinition([]common.Directive{
			builder.NewPlanDefinition(loginBuilder, pkgs, common.TagList{"level3"}),
			common.DirectiveRunCommand("interactive"),
		}, nil, nil, "", loginCpuCores, loginMemorySize, loginStorageSize, "ssh")

		if _, err := db.Build(db.NewBuildContext(def), def, common.BuildOptions{
			AlwaysRebuild: true,
		}); err != nil {
			return err
		}

		return nil
	},
}

func init() {
	loginCmd.PersistentFlags().StringVarP(&loginBuilder, "builder", "b", "", "the container builder used to construct the virtual machine")
	loginCmd.PersistentFlags().IntVar(&loginCpuCores, "cpu", 1, "the number of CPU cores to allocate to the virtual machine")
	loginCmd.PersistentFlags().IntVar(&loginMemorySize, "ram", 1024, "the amount of ram in the virtual machine in megabytes")
	loginCmd.PersistentFlags().IntVar(&loginStorageSize, "storage", 1024, "the amount of storage to allocate in the virtual machine in megabytes")
	loginCmd.MarkFlagRequired("builder")
	rootCmd.AddCommand(loginCmd)
}

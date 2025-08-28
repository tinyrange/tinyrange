package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/log"
	"github.com/tinyrange/tinyrange/pkg/vmm/accelerate"
)

var envCmd = &cobra.Command{
	Use:   "env",
	Short: "Manage the TinyRange environment",
}

var buildDirCmd = &cobra.Command{
	Use:   "build-dir",
	Short: "Print the build directory",
	RunE: func(cmd *cobra.Command, args []string) error {
		buildDir, err := getBuildDir()
		if err != nil {
			return err
		}

		fmt.Fprintf(os.Stdout, "%s", buildDir)

		return nil
	},
}

var checkHardwareAccelerationCmd = &cobra.Command{
	Use:   "check-hv",
	Short: "Check if hardware acceleration is available",
	RunE: func(cmd *cobra.Command, args []string) error {
		supported := accelerate.SupportsAcceleration(log.Default())
		if supported {
			fmt.Println("Hardware acceleration is available")
			os.Exit(0)
		} else {
			fmt.Println("Hardware acceleration is not available, or not supported on this platform")
			os.Exit(1)
		}
		return nil // unreachable
	},
}

var getDefaultVMMCmd = &cobra.Command{
	Use:   "get-default-vmm",
	Short: "Print the default VMM",
	RunE: func(cmd *cobra.Command, args []string) error {
		exe, err := common.GetAdjacentExecutable("tinyrange_qemu", "tinyqemu/tinyrange_qemu")
		if err != nil {
			return err
		}

		fmt.Fprintf(os.Stdout, "%s", exe)

		return nil
	},
}

var getAutoScaleConfigCmd = &cobra.Command{
	Use:   "get-auto-scale-config",
	Short: "Print the auto scale configuration (CPU, RAM, Storage)",
	RunE: func(cmd *cobra.Command, args []string) error {
		cpus, ram, err := common.GetCPUAndMemoryAutoScaleConfig()
		if err != nil {
			return err
		}

		fmt.Fprintf(os.Stdout, "CPUs: %d\n", cpus)
		fmt.Fprintf(os.Stdout, "RAM: %d\n", ram)

		return nil
	},
}

func init() {
	rootCmd.AddCommand(envCmd)

	envCmd.AddCommand(buildDirCmd)
	envCmd.AddCommand(checkHardwareAccelerationCmd)
	envCmd.AddCommand(getDefaultVMMCmd)
	envCmd.AddCommand(getAutoScaleConfigCmd)
}

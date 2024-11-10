package cli

import (
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/tinyrange/tinyrange/pkg/ccvm"
	"github.com/tinyrange/tinyrange/pkg/filesystem/ext4"
	"github.com/tinyrange/vm"
)

var ccvmCmd = &cobra.Command{
	Use:   "ccvm",
	Short: "Run an embedded virtual machine",
	RunE: func(cmd *cobra.Command, args []string) error {
		vMem := vm.NewVirtualMemory(512*1024*1024, 4096)

		vmFs, err := ext4.CreateExt4Filesystem(vMem, 0, 512*1024*1024)
		if err != nil {
			return err
		}

		rvInit, err := os.ReadFile("build/tinyrange_init_riscv64")
		if err != nil {
			return err
		}

		if err := vmFs.CreateFile("/init", vm.RawRegion(rvInit)); err != nil {
			return err
		}

		if err := vmFs.Chmod("/init", 0755); err != nil {
			return err
		}

		contents, err := io.ReadAll(io.NewSectionReader(vMem, 0, 512*1024*1024))
		if err != nil {
			return err
		}

		return ccvm.RunVirtualMachine(256*1024*1024, contents, ccvm.LINUX_KERNEL, "console=hvc0 root=/dev/vda rw init=/init tinyrange.verbose=on")
	},
}

func init() {
	rootCmd.AddCommand(ccvmCmd)
}

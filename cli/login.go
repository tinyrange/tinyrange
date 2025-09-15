package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Start a simple VM from a login configuration",
	RunE: func(cmd *cobra.Command, args []string) error {
		// login.Hello()
		return fmt.Errorf("not implemented")
	},
}

func init() {
	rootCmd.AddCommand(loginCmd)
}

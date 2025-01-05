package cli

import (
	"github.com/spf13/cobra"
	"github.com/tinyrange/tinyrange/pkg/trweb"
)

var webCmd = &cobra.Command{
	Use:   "web",
	Short: "Run a web interface",
	RunE: func(cmd *cobra.Command, args []string) error {
		db, err := newDb()
		if err != nil {
			return err
		}

		svr := trweb.New(db)

		return svr.Run("127.0.0.1:5123")
	},
}

func init() {
	rootCmd.AddCommand(webCmd)
}

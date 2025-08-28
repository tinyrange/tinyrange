package cli

import (
	"github.com/spf13/cobra"

	"github.com/tinyrange/tinyrange/pkg/server"
)

var (
	serveAddress string = "127.0.0.1:5123"
)

var serveCommand = &cobra.Command{
	Use:   "serve",
	Short: "Start the TinyRange server",
	RunE: func(cmd *cobra.Command, args []string) error {
		db, err := newDb()
		if err != nil {
			return err
		}

		server := server.New(db)

		if err := server.ListenAndServe(serveAddress); err != nil {
			return err
		}

		return nil
	},
}

func init() {
	serveCommand.PersistentFlags().StringVar(&serveAddress, "address", serveAddress, "Address to listen on for the server")
	rootCmd.AddCommand(serveCommand)
}

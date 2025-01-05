package cli

import (
	"github.com/spf13/cobra"
)

var (
	distributionAddr string
)

var distributionCmd = &cobra.Command{
	Use:   "distribution",
	Short: "Start a web server serving the distributable artifacts in the build cache.",
	RunE: func(cmd *cobra.Command, args []string) error {
		db, err := newDb()
		if err != nil {
			return err
		}

		return db.RunDistributionServer(distributionAddr)
	},
}

func init() {
	distributionCmd.PersistentFlags().StringVar(&distributionAddr, "addr", "localhost:5123", "The address to listen on.")
	rootCmd.AddCommand(distributionCmd)
}

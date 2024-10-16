package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/config"
)

var queryBuilder string

var queryCmd = &cobra.Command{
	Use:   "query",
	Short: "Search the package index for a package",
	RunE: func(cmd *cobra.Command, args []string) error {
		if queryBuilder == "" {
			return fmt.Errorf("please specify a builder")
		}

		db, err := newDb()
		if err != nil {
			return err
		}

		ctx := db.NewBuildContext(nil)

		b, err := db.GetContainerBuilder(ctx, queryBuilder, config.HostArchitecture)
		if err != nil {
			return err
		}

		var q common.PackageQuery

		if len(args) > 0 {
			q, err = common.ParsePackageQuery(args[0])
			if err != nil {
				return err
			}
		} else {
			q, err = common.ParsePackageQuery("*")
			if err != nil {
				return err
			}
		}

		q.MatchDirect = true
		q.MatchPartialName = true

		results, err := b.Search(q)
		if err != nil {
			return err
		}

		for _, result := range results {
			fmt.Printf("%s\n", result)
		}

		return nil
	},
}

func init() {
	queryCmd.PersistentFlags().StringVarP(&queryBuilder, "builder", "b", "", "the container builder to query from")
	queryCmd.MarkFlagRequired("builder")
	rootCmd.AddCommand(queryCmd)
}

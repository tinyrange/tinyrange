package cli

import (
    "encoding/json"
    "fmt"

	"github.com/spf13/cobra"

    "github.com/tinyrange/tinyrange/pkg/common"
    "github.com/tinyrange/tinyrange/pkg/hash"
    pb "github.com/tinyrange/tinyrange/pkg/proto"
    gp "google.golang.org/protobuf/proto"
    pj "google.golang.org/protobuf/encoding/protojson"
)

var inspectCmd = &cobra.Command{
	Use:   "inspect <hash>",
	Short: "Inspect a build artifact",
	RunE: func(cmd *cobra.Command, args []string) error {
		db, err := newDb()
		if err != nil {
			return err
		}

		if len(args) != 1 {
			return fmt.Errorf("please specify a hash")
		}

		art, err := db.Builder().Filesystem().GetBuildDirectory(hash.Hash(args[0]))
		if err != nil {
			return fmt.Errorf("failed to get build artifact: %w", err)
		}

		var receipt common.BuildReceipt

		// Read the definition from the artifact
		defBytes, err := art.ReadDefinition()
		if err != nil {
			return fmt.Errorf("failed to get definition: %w", err)
		}

        // Definition is protobuf; render as JSON for display
        var bd pb.BuildDefinition
        if err := gp.Unmarshal(defBytes, &bd); err != nil {
            return fmt.Errorf("failed to parse definition: %w", err)
        }
        j, err := (pj.MarshalOptions{UseProtoNames: true, Multiline: true, Indent: "  "}).Marshal(&bd)
        if err != nil { return fmt.Errorf("failed to marshal definition json: %w", err) }
        fmt.Println(string(j))

		receiptBytes, err := art.ReadReceipt()
		if err != nil {
			return fmt.Errorf("failed to get receipt: %w", err)
		}

		if err := json.Unmarshal(receiptBytes, &receipt); err != nil {
			return fmt.Errorf("failed to unmarshal receipt: %w", err)
		}

		// stringify the receipt
		receiptString, err := json.MarshalIndent(receipt, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal receipt: %w", err)
		}
		fmt.Println(string(receiptString))

		return nil
	},
}

func init() {
	rootCmd.AddCommand(inspectCmd)
}

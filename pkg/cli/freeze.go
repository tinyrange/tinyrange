package cli

import (
	"encoding/json"
	"fmt"

	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/hash"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var freezeCmd = &cobra.Command{
	Use:   "freeze <hash>",
	Short: "Dump a build definition and its dependencies as YAML",
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 {
			return fmt.Errorf("please specify a hash")
		}

		db, err := newDb()
		if err != nil {
			return err
		}

		rootHash := hash.Hash(args[0])
		builder := db.Builder()

		type serialized struct {
			Type   string         `yaml:"type"`
			Params map[string]any `yaml:"params"`
		}

		defs := make(map[string]serialized)
		defDb := hash.NewDefinitionDatabase(nil)

		var walk func(hash.Hash) error
		walk = func(h hash.Hash) error {
			hStr := h.String()
			if _, ok := defs[hStr]; ok {
				return nil
			}

			def, err := builder.GetDefinitionByHash(h)
			if err != nil {
				return fmt.Errorf("failed to get definition %s: %w", hStr, err)
			}

			data, err := defDb.MarshalDefinition(def)
			if err != nil {
				return err
			}
			var tmp struct {
				TypeName string         `json:"TypeName"`
				Params   map[string]any `json:"Params"`
			}
			if err := json.Unmarshal(data, &tmp); err != nil {
				return err
			}
			defs[hStr] = serialized{Type: tmp.TypeName, Params: tmp.Params}

			deps, err := def.Dependencies()
			if err != nil {
				return err
			}
			for _, dep := range deps {
				depHash, err := defDb.HashDefinition(dep)
				if err != nil {
					return err
				}
				if err := walk(depHash); err != nil {
					return err
				}
			}

			dir, err := builder.Filesystem().GetBuildDirectory(h)
			if err != nil {
				return fmt.Errorf("failed to open build directory for %s: %w", hStr, err)
			}
			receiptBytes, err := dir.ReadReceipt()
			if err != nil {
				return fmt.Errorf("failed to read receipt for %s: %w", hStr, err)
			}
			var receipt common.BuildReceipt
			if err := json.Unmarshal(receiptBytes, &receipt); err != nil {
				return fmt.Errorf("failed to unmarshal receipt for %s: %w", hStr, err)
			}
			for _, req := range receipt.Requirements {
				if err := walk(req); err != nil {
					return err
				}
			}

			return nil
		}

		if err := walk(rootHash); err != nil {
			return err
		}

		out := struct {
			Root        string                `yaml:"root"`
			Definitions map[string]serialized `yaml:"definitions"`
		}{Root: rootHash.String(), Definitions: defs}

		b, err := yaml.Marshal(out)
		if err != nil {
			return err
		}
		fmt.Print(string(b))
		return nil
	},
}

func init() {
	rootCmd.AddCommand(freezeCmd)
}

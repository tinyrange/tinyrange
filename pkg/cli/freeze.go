package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/hash"
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
		def, err := db.Builder().GetDefinitionByHash(rootHash)
		if err != nil {
			return fmt.Errorf("failed to get definition: %w", err)
		}

		type serialized struct {
			Type   string         `yaml:"type"`
			Params map[string]any `yaml:"params"`
		}

		defs := make(map[string]serialized)
		defDb := hash.NewDefinitionDatabase(nil)

		var walk func(common.BuildDefinition) error
		walk = func(d common.BuildDefinition) error {
			h, err := defDb.HashDefinition(d)
			if err != nil {
				return err
			}
			hStr := h.String()
			if _, ok := defs[hStr]; ok {
				return nil
			}

			data, err := defDb.MarshalDefinition(d)
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

			deps, err := d.Dependencies()
			if err != nil {
				return err
			}
			for _, dep := range deps {
				if err := walk(dep); err != nil {
					return err
				}
			}
			return nil
		}

		if err := walk(def); err != nil {
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

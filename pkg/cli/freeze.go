package cli

import (
    "encoding/json"
    "fmt"

    "github.com/tinyrange/tinyrange/pkg/common"
    "github.com/tinyrange/tinyrange/pkg/hash"
    pb "github.com/tinyrange/tinyrange/pkg/proto"

    "github.com/spf13/cobra"
    "gopkg.in/yaml.v3"
    gp "google.golang.org/protobuf/proto"
    pj "google.golang.org/protobuf/encoding/protojson"
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
            if err != nil { return err }

            var bd pb.BuildDefinition
            if err := gp.Unmarshal(data, &bd); err != nil {
                return fmt.Errorf("failed to parse definition protobuf: %w", err)
            }

            var typeName string
            var paramsMsg gp.Message
            switch v := bd.GetDefinition().(type) {
            case *pb.BuildDefinition_BuildFs:
                typeName = "BuildFsParameters"
                paramsMsg = v.BuildFs
            case *pb.BuildDefinition_BuildVm:
                typeName = "BuildVmParameters"
                paramsMsg = v.BuildVm
            case *pb.BuildDefinition_BuildEmulator:
                typeName = "BuildEmulatorParameters"
                paramsMsg = v.BuildEmulator
            case *pb.BuildDefinition_DecompressFile:
                typeName = "DecompressFileParameters"
                paramsMsg = v.DecompressFile
            case *pb.BuildDefinition_FetchHttp:
                typeName = "FetchHttpParameters"
                paramsMsg = v.FetchHttp
            case *pb.BuildDefinition_RegistryRequest:
                typeName = "RegistryRequestParameters"
                paramsMsg = v.RegistryRequest
            case *pb.BuildDefinition_FetchOciImage:
                typeName = "FetchOciImageParameters"
                paramsMsg = v.FetchOciImage
            case *pb.BuildDefinition_FetchCvmfs:
                typeName = "FetchCVMFSParameters"
                paramsMsg = v.FetchCvmfs
            case *pb.BuildDefinition_ReadOciImage:
                typeName = "ReadOciImageParameters"
                paramsMsg = v.ReadOciImage
            case *pb.BuildDefinition_File:
                typeName = "FileParameters"
                paramsMsg = v.File
            case *pb.BuildDefinition_ConstantHash:
                typeName = "ConstantHashParameters"
                paramsMsg = v.ConstantHash
            case *pb.BuildDefinition_ExtractFile:
                typeName = "ExtractFileParameters"
                paramsMsg = v.ExtractFile
            case *pb.BuildDefinition_Plan:
                typeName = "PlanParameters"
                paramsMsg = v.Plan
            case *pb.BuildDefinition_ReadArchive:
                typeName = "ReadArchiveParameters"
                paramsMsg = v.ReadArchive
            case *pb.BuildDefinition_Star:
                typeName = "StarParameters"
                paramsMsg = v.Star
            default:
                return fmt.Errorf("unsupported definition type: %T", bd.GetDefinition())
            }

            // Convert params message to generic map using protojson
            var paramsMap map[string]any
            if paramsMsg != nil {
                b, err := (pj.MarshalOptions{UseProtoNames: true}).Marshal(paramsMsg)
                if err != nil { return err }
                if err := json.Unmarshal(b, &paramsMap); err != nil { return err }
            }
            defs[hStr] = serialized{Type: typeName, Params: paramsMap}

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

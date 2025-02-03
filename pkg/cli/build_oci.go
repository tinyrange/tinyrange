package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/parser"
	"github.com/spf13/cobra"
	"github.com/tinyrange/tinyrange/pkg/builder"
	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/config"
	"github.com/tinyrange/tinyrange/pkg/feature"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/path"
)

type buildOciContext struct {
	layers     []common.Directive
	workdir    string
	entrypoint string
}

func (c *buildOciContext) resolve(p string) string {
	if c.workdir == "" {
		return p
	}

	return path.Unix.Join(c.workdir, p)
}

var buildOciCmd = &cobra.Command{
	Use:   "build-oci <tag> <dockerfile> <context>",
	Short: "Build and tag an OCI image",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !feature.HasFeature(feature.FeatureBuildOci) {
			return fmt.Errorf("build-oci is not enabled")
		}

		if len(args) != 3 {
			return fmt.Errorf("please specify <tag> <dockerfile> <context>")
		}

		tag, dockerfile, context := args[0], args[1], args[2]

		// Open the context directory
		osDir := filesystem.NewLocalDirectory(context)

		db, err := newDb()
		if err != nil {
			return err
		}

		_ = osDir

		// parse the dockerfile generating a series of layers.
		r, err := os.Open(dockerfile)
		if err != nil {
			return err
		}
		defer r.Close()

		res, err := parser.Parse(r)
		if err != nil {
			return err
		}

		res.PrintWarnings(os.Stderr)

		contexts := make(map[string]*buildOciContext)

		currentContext := &buildOciContext{}

		contexts[""] = currentContext

		for _, child := range res.AST.Children {
			switch strings.ToLower(child.Value) {
			case "from":
				var args []string
				for arg := child.Next; arg != nil; arg = arg.Next {
					args = append(args, arg.Value)
				}

				var contextName string
				if len(args) == 3 && strings.ToLower(args[1]) == "as" {
					contextName = args[2]
				}

				registry, image, tag, err := builder.ParseOciImage(args[0])
				if err != nil {
					return fmt.Errorf("failed to parse %s: %w", child.Next.Value, err)
				}

				ociArch, err := builder.ToOciArchitecture(config.HostArchitecture)
				if err != nil {
					return fmt.Errorf("failed to convert %s to OCI architecture: %w", config.HostArchitecture, err)
				}

				ociDef := builder.Factory.NewFetchOCIImageDefinition(
					registry, image, tag, ociArch,
				)

				currentContext = &buildOciContext{}
				currentContext.layers = append(currentContext.layers, ociDef)
				contexts[contextName] = currentContext
			case "workdir":
				currentContext.workdir = child.Next.Value
			case "copy":
				var fromContext string
				for _, flags := range child.Flags {
					if strings.HasPrefix(flags, "--from=") {
						from := strings.TrimPrefix(flags, "--from=")

						fromContext = from
					}
				}

				var args []string
				for arg := child.Next; arg != nil; arg = arg.Next {
					args = append(args, arg.Value)
				}

				srcFiles := args[:len(args)-1]
				dst := args[len(args)-1]

				var dir []common.Directive

				for _, src := range srcFiles {
					if fromContext != "" {
						from, ok := contexts[fromContext]
						if !ok {
							return fmt.Errorf("unknown context: %s", fromContext)
						}

						ark := builder.Factory.NewBuildFsDefinition(from.layers, "archive")

						file := builder.Factory.NewExtractFileDefinition(ark, src)

						filename := currentContext.resolve(dst)

						dir = append(dir, common.DirectiveAddFile{
							Filename:   filename,
							Definition: file,
						})
					} else {
						file, err := filesystem.OpenPath(osDir, src)
						if err != nil {
							return fmt.Errorf("failed to open %s: %w", src, err)
						}

						def, err := builder.Factory.NewDefinitionFromFile(file)
						if err != nil {
							return fmt.Errorf("failed to create definition from %s: %w", src, err)
						}

						filename := path.Unix.Join(currentContext.resolve(dst), file.Name)

						dir = append(dir, common.DirectiveAddFile{
							Filename:   filename,
							Definition: def,
						})
					}
				}

				fs := builder.Factory.NewBuildFsDefinition(dir, "archive")

				layersCopy := make([]common.Directive, len(currentContext.layers))
				copy(layersCopy, currentContext.layers)

				currentContext.layers = append(layersCopy, fs)
			case "run":
				var args []string
				for arg := child.Next; arg != nil; arg = arg.Next {
					args = append(args, arg.Value)
				}

				// join the args into a single string
				cmd := strings.Join(args, " ")

				run := builder.Factory.NewBuildVmDefinition(
					append( // append the current layers
						currentContext.layers,
						common.DirectiveRunCommand{Command: cmd},
					),
					nil, nil, // kernel, initrd
					"/init/changed.archive", // output file
					1,                       // cpu count
					1024,                    // memory
					config.HostArchitecture, // architecture
					config.HostArchitecture, // target architecture
					1024,                    // storage
					"ssh",                   // interaction
					false,                   // debug
				)

				layersCopy := make([]common.Directive, len(currentContext.layers))
				copy(layersCopy, currentContext.layers)

				currentContext.layers = append(layersCopy, run)
			case "add":
				var args []string
				for arg := child.Next; arg != nil; arg = arg.Next {
					args = append(args, arg.Value)
				}

				srcFiles := args[:len(args)-1]
				dst := args[len(args)-1]

				var dir []common.Directive

				for _, src := range srcFiles {
					file, err := filesystem.OpenPath(osDir, src)
					if err != nil {
						return fmt.Errorf("failed to open %s: %w", src, err)
					}

					def, err := builder.Factory.NewDefinitionFromFile(file)
					if err != nil {
						return fmt.Errorf("failed to create definition from %s: %w", src, err)
					}

					filename := path.Unix.Join(currentContext.resolve(dst), file.Name)

					dir = append(dir, common.DirectiveAddFile{
						Filename:   filename,
						Definition: def,
					})
				}

				fs := builder.Factory.NewBuildFsDefinition(dir, "archive")

				layersCopy := make([]common.Directive, len(currentContext.layers))
				copy(layersCopy, currentContext.layers)

				currentContext.layers = append(layersCopy, fs)
			case "entrypoint":
				var args []string
				for arg := child.Next; arg != nil; arg = arg.Next {
					args = append(args, arg.Value)
				}

				currentContext.entrypoint = strings.Join(args, " ")

			default:
				return fmt.Errorf("unsupported command: %s", child.Value)
			}
		}

		top := builder.Factory.NewBuildVmDefinition(
			append( // append the entrypoint command
				currentContext.layers,
				common.DirectiveRunCommand{Command: currentContext.entrypoint},
			),
			nil, nil, // kernel, initrd
			"",                      // output file
			1,                       // cpu count
			1024,                    // memory
			config.HostArchitecture, // architecture
			config.HostArchitecture, // target architecture
			1024,                    // storage
			"ssh",                   // interaction
			false,                   // debug
		)

		// Build the top-level definition
		if _, err := db.Builder().Build(top, common.BuildOptions{
			AlwaysRebuild: true,
		}); err != nil {
			return err
		}

		_ = tag

		return nil
	},
}

func init() {
	// Command is WIP and not yet ready for use.
	rootCmd.AddCommand(buildOciCmd)
}

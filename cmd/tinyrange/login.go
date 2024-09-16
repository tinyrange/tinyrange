package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime/pprof"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tinyrange/tinyrange/pkg/builder"
	"github.com/tinyrange/tinyrange/pkg/common"
	cfg "github.com/tinyrange/tinyrange/pkg/config"
	"gopkg.in/yaml.v3"
)

const DEFAuLT_BUILDER = "alpine@3.20"

func detectArchiveExtractor(base common.BuildDefinition, filename string) (common.BuildDefinition, error) {
	if builder.ReadArchiveSupportsExtracting(filename) {
		return builder.NewReadArchiveBuildDefinition(base, filename), nil
	} else {
		return nil, fmt.Errorf("no extractor for %s", filename)
	}
}

func sha256HashFromReader(r io.Reader) (string, error) {
	h := sha256.New()

	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

func sha256HashFromFile(filename string) (string, error) {
	f, err := os.Open(filename)
	if err != nil {
		return "", err
	}
	defer f.Close()

	return sha256HashFromReader(f)
}

var CURRENT_CONFIG_VERSION = 1

type loginConfig struct {
	Version      int      `json:"version" yaml:"version"`
	Builder      string   `json:"builder" yaml:"builder"`
	Architecture string   `json:"architecture,omitempty" yaml:"architecture,omitempty"`
	Commands     []string `json:"commands,omitempty" yaml:"commands,omitempty"`
	Files        []string `json:"files,omitempty" yaml:"files,omitempty"`
	Archives     []string `json:"archives,omitempty" yaml:"archives,omitempty"`
	Output       string   `json:"output,omitempty" yaml:"output,omitempty"`
	Packages     []string `json:"packages,omitempty" yaml:"packages,omitempty"`
	Macros       []string `json:"macros,omitempty" yaml:"macros,omitempty"`
	Environment  []string `json:"environment,omitempty" yaml:"environment,omitempty"`
	NoScripts    bool     `json:"no_scripts,omitempty" yaml:"no_scripts,omitempty"`
	Init         string   `json:"init,omitempty" yaml:"init,omitempty"`

	// private configs that have to be set on the command line.
	cpuCores          int
	memorySize        int
	storageSize       int
	debug             bool
	writeRoot         string
	experimentalFlags []string
	hash              bool
}

func (config *loginConfig) run() error {
	if config.Version > CURRENT_CONFIG_VERSION {
		return fmt.Errorf("attempt to run config version %d on TinyRange version %d", config.Version, CURRENT_CONFIG_VERSION)
	}

	db, err := newDb()
	if err != nil {
		return err
	}

	if config.Builder == "list" {
		for name, builder := range db.ContainerBuilders {
			fmt.Printf(" - %s - %s\n", name, builder.DisplayName)
		}

		return nil
	}

	var directives []common.Directive

	if config.Builder == "" {
		return fmt.Errorf("please specify a builder")
	}

	var tags common.TagList

	tags = append(tags, "level3", "defaults")

	if config.NoScripts || config.writeRoot != "" {
		tags = append(tags, "noScripts")
	}

	arch, err := cfg.ArchitectureFromString(config.Architecture)
	if err != nil {
		return err
	}

	for _, filename := range config.Files {
		if strings.HasPrefix(filename, "http://") || strings.HasPrefix(filename, "https://") {
			parsed, err := url.Parse(filename)
			if err != nil {
				return err
			}

			base := path.Base(parsed.Path)

			directives = append(directives, common.DirectiveAddFile{
				Definition: builder.NewFetchHttpBuildDefinition(filename, 0, nil),
				Filename:   path.Join("/root", base),
			})
		} else {
			absPath, err := filepath.Abs(filename)
			if err != nil {
				return err
			}

			directives = append(directives, common.DirectiveLocalFile{
				HostFilename: absPath,
				Filename:     path.Join("/root", filepath.Base(absPath)),
			})
		}
	}

	for _, filename := range config.Archives {
		var def common.BuildDefinition
		if strings.HasPrefix(filename, "http://") || strings.HasPrefix(filename, "https://") {
			def = builder.NewFetchHttpBuildDefinition(filename, 0, nil)

			parsed, err := url.Parse(filename)
			if err != nil {
				return err
			}

			filename = parsed.Path
		} else {
			hash, err := sha256HashFromFile(filename)
			if err != nil {
				return err
			}

			def = builder.NewConstantHashDefinition(hash, func() (io.ReadCloser, error) {
				return os.Open(filename)
			})
		}

		ark, err := detectArchiveExtractor(def, filename)
		if err != nil {
			return err
		}

		directives = append(directives, common.DirectiveArchive{Definition: ark, Target: "/root"})
	}

	var pkgs []common.PackageQuery

	for _, arg := range config.Packages {
		q, err := common.ParsePackageQuery(arg)
		if err != nil {
			return err
		}

		pkgs = append(pkgs, q)
	}

	planDirective, err := builder.NewPlanDefinition(config.Builder, arch, pkgs, tags)
	if err != nil {
		return err
	}

	macroCtx := db.NewMacroContext()
	macroCtx.AddBuilder("default", planDirective)

	for _, macro := range config.Macros {
		m, err := db.GetMacroByShorthand(macroCtx, macro)
		if err != nil {
			return err
		}

		def, err := m.Call(macroCtx)
		if err != nil {
			return err
		}

		if star, ok := def.(*common.StarDirective); ok {
			def = star.Directive
		}

		if dir, ok := def.(common.Directive); ok {
			directives = append(directives, dir)
		} else {
			return fmt.Errorf("handling of macro def %T not implemented", def)
		}
	}

	if config.writeRoot != "" {
		directives = append(directives, common.DirectiveBuiltin{Name: "init", Architecture: string(arch), GuestFilename: "init"})

		def := builder.NewBuildFsDefinition(directives, "tar")

		ctx := db.NewBuildContext(def)

		f, err := db.Build(ctx, def, common.BuildOptions{})
		if err != nil {
			slog.Error("fatal", "err", err)
			os.Exit(1)
		}

		fh, err := f.Open()
		if err != nil {
			return err
		}
		defer fh.Close()

		out, err := os.Create(path.Base(config.writeRoot))
		if err != nil {
			return err
		}
		defer out.Close()

		if _, err := io.Copy(out, fh); err != nil {
			return err
		}

		return nil
	}

	if len(config.Commands) == 0 && config.Init == "" {
		directives = append(directives, common.DirectiveRunCommand{Command: "interactive"})
	} else {
		for _, cmd := range config.Commands {
			directives = append(directives, common.DirectiveRunCommand{Command: cmd})
		}
	}

	if len(config.Environment) > 0 {
		directives = append(directives, common.DirectiveEnvironment{Variables: config.Environment})
	}

	interaction := "ssh"

	directives, err = common.FlattenDirectives(directives, common.SpecialDirectiveHandlers{
		AddPackage: func(dir common.DirectiveAddPackage) error {
			planDirective, err = planDirective.AddPackage(dir.Name)
			if err != nil {
				return err
			}

			return nil
		},
		Interaction: func(dir common.DirectiveInteraction) error {
			interaction = dir.Interaction

			return nil
		},
	})
	if err != nil {
		return err
	}

	directives = append([]common.Directive{planDirective}, directives...)

	if config.Init != "" {
		interaction = "init," + config.Init
	}

	def := builder.NewBuildVmDefinition(
		directives,
		nil, nil,
		config.Output,
		config.cpuCores, config.memorySize, arch,
		config.storageSize,
		interaction, config.debug,
	)

	if config.Output != "" {
		ctx := db.NewBuildContext(def)

		defHash, err := db.HashDefinition(def)
		if err != nil {
			slog.Error("fatal", "err", err)
			os.Exit(1)
		}

		f, err := db.Build(ctx, def, common.BuildOptions{})
		if err != nil {
			slog.Error("fatal", "err", err)
			os.Exit(1)
		}

		fh, err := f.Open()
		if err != nil {
			return err
		}
		defer fh.Close()

		out, err := os.Create(path.Base(config.Output))
		if err != nil {
			return err
		}
		defer out.Close()

		if _, err := io.Copy(out, fh); err != nil {
			return err
		}

		if config.hash {
			slog.Info("wrote output", "filename", path.Base(config.Output), "hash", defHash)
		}

		return nil
	} else {
		ctx := db.NewBuildContext(def)
		if _, err := db.Build(ctx, def, common.BuildOptions{
			AlwaysRebuild: true,
		}); err != nil {
			slog.Error("fatal", "err", err)
			os.Exit(1)
		}

		// if common.IsVerbose() {
		// 	ctx.DisplayTree()
		// }

		return nil
	}
}

var currentConfig loginConfig = loginConfig{Version: CURRENT_CONFIG_VERSION}

var (
	loginSaveConfig string
	loginLoadConfig string
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Start a virtual machine with a builder and a list of packages",
	RunE: func(cmd *cobra.Command, args []string) error {
		if rootCpuProfile != "" {
			f, err := os.Create(rootCpuProfile)
			if err != nil {
				return err
			}
			pprof.StartCPUProfile(f)
			defer pprof.StopCPUProfile()
		}

		if len(currentConfig.experimentalFlags) > 0 {
			if err := common.SetExperimental(currentConfig.experimentalFlags); err != nil {
				return err
			}
		}

		currentConfig.Packages = args

		if loginLoadConfig != "" {
			f, err := os.Open(loginLoadConfig)
			if err != nil {
				return err
			}
			defer f.Close()

			dec := yaml.NewDecoder(f)

			if err := dec.Decode(&currentConfig); err != nil {
				return err
			}
		}

		if loginSaveConfig != "" {
			cfg, err := yaml.Marshal(&currentConfig)
			if err != nil {
				return err
			}

			return os.WriteFile(loginSaveConfig, cfg, os.FileMode(0644))
		} else {
			return currentConfig.run()
		}
	},
}

func init() {
	// config flags
	loginCmd.PersistentFlags().StringVarP(&loginSaveConfig, "save-config", "w", "", "Write the config to a given file and don't run it.")
	loginCmd.PersistentFlags().StringVarP(&loginLoadConfig, "load-config", "c", "", "Load the config from a file and run it.")

	// public flags (saved to config)
	loginCmd.PersistentFlags().StringVarP(&currentConfig.Builder, "builder", "b", DEFAuLT_BUILDER, "The container builder used to construct the virtual machine.")
	loginCmd.PersistentFlags().StringArrayVarP(&currentConfig.Commands, "exec", "E", []string{}, "Run a different command rather than dropping into a shell.")
	loginCmd.PersistentFlags().StringVar(&currentConfig.Init, "init", "", "Replace the init system with a different command.")
	loginCmd.PersistentFlags().BoolVar(&currentConfig.NoScripts, "no-scripts", false, "Disable script execution.")
	loginCmd.PersistentFlags().StringArrayVarP(&currentConfig.Files, "file", "f", []string{}, "Specify local files/URLs to be copied into the virtual machine. URLs will be downloaded to the build directory first.")
	loginCmd.PersistentFlags().StringArrayVarP(&currentConfig.Archives, "archive", "a", []string{}, "Specify archives to be copied into the virtual machine. A copy will be made in the build directory.")
	loginCmd.PersistentFlags().StringVarP(&currentConfig.Output, "output", "o", "", "Write the specified file from the guest to the host.")
	loginCmd.PersistentFlags().StringArrayVarP(&currentConfig.Environment, "environment", "e", []string{}, "Add environment variables to the VM.")
	loginCmd.PersistentFlags().StringArrayVarP(&currentConfig.Macros, "macro", "m", []string{}, "Add macros to the VM.")
	loginCmd.PersistentFlags().StringVar(&currentConfig.Architecture, "arch", "", "Override the CPU architecture of the machine. This will use emulation with a performance hit.")

	// private flags (need to set on command line)
	loginCmd.PersistentFlags().IntVar(&currentConfig.cpuCores, "cpu", 1, "The number of CPU cores to allocate to the virtual machine.")
	loginCmd.PersistentFlags().IntVar(&currentConfig.memorySize, "ram", 1024, "The amount of ram in the virtual machine in megabytes.")
	loginCmd.PersistentFlags().IntVar(&currentConfig.storageSize, "storage", 1024, "The amount of storage to allocate in the virtual machine in megabytes.")
	loginCmd.PersistentFlags().BoolVar(&currentConfig.debug, "debug", false, "Redirect output from the hypervisor to the host. the guest will exit as soon as the VM finishes startup.")
	loginCmd.PersistentFlags().StringVar(&currentConfig.writeRoot, "write-root", "", "Write the root filesystem as a .tar.gz archive.")
	loginCmd.PersistentFlags().BoolVar(&currentConfig.hash, "hash", false, "print the hash of the definition generated after the machine has exited.")
	loginCmd.PersistentFlags().StringArrayVar(&currentConfig.experimentalFlags, "experimental", []string{}, "Add experimental flags.")
	rootCmd.AddCommand(loginCmd)
}

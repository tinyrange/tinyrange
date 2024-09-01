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
	"runtime"
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
	Environment  []string `json:"environment,omitempty" yaml:"environment,omitempty"`
	NoScripts    bool     `json:"no_scripts,omitempty" yaml:"no_scripts,omitempty"`

	// private configs that have to be set on the command line.
	cpuCores     int
	memorySize   int
	storageSize  int
	debug        bool
	writeRoot    string
	runRoot      string
	runContainer bool
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

	var pkgs []common.PackageQuery

	for _, arg := range config.Packages {
		q, err := common.ParsePackageQuery(arg)
		if err != nil {
			return err
		}

		pkgs = append(pkgs, q)
	}

	var dir []common.Directive

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

	planDirective, err := builder.NewPlanDefinition(config.Builder, arch, pkgs, tags)
	if err != nil {
		return err
	}

	dir = append(dir, planDirective)

	for _, filename := range config.Files {
		if strings.HasPrefix(filename, "http://") || strings.HasPrefix(filename, "https://") {
			parsed, err := url.Parse(filename)
			if err != nil {
				return err
			}

			base := path.Base(parsed.Path)

			dir = append(dir, common.DirectiveAddFile{
				Definition: builder.NewFetchHttpBuildDefinition(filename, 0),
				Filename:   path.Join("/root", base),
			})
		} else {
			absPath, err := filepath.Abs(filename)
			if err != nil {
				return err
			}

			dir = append(dir, common.DirectiveLocalFile{
				HostFilename: absPath,
				Filename:     path.Join("/root", filepath.Base(absPath)),
			})
		}
	}

	for _, filename := range config.Archives {
		var def common.BuildDefinition
		if strings.HasPrefix(filename, "http://") || strings.HasPrefix(filename, "https://") {
			def = builder.NewFetchHttpBuildDefinition(filename, 0)

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

		dir = append(dir, common.DirectiveArchive{Definition: ark, Target: "/root"})
	}

	if config.writeRoot != "" {
		dir = append(dir, common.DirectiveBuiltin{Name: "init", GuestFilename: "init"})

		def := builder.NewBuildFsDefinition(dir, "tar")

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

	if len(config.Commands) == 0 {
		dir = append(dir, common.DirectiveRunCommand{Command: "interactive"})
	} else {
		for _, cmd := range config.Commands {
			dir = append(dir, common.DirectiveRunCommand{Command: cmd})
		}
	}

	dir = append(dir, common.DirectiveEnvironment{Variables: config.Environment})

	if config.runRoot != "" {
		dir = append(dir, common.DirectiveBuiltin{Name: "init", GuestFilename: "init"})

		def := builder.NewBuildFsDefinition(dir, "fragments")

		ctx := db.NewBuildContext(def)

		f, err := db.Build(ctx, def, common.BuildOptions{})
		if err != nil {
			slog.Error("fatal", "err", err)
			os.Exit(1)
		}

		frags, err := builder.ParseFragmentsBuilderResult(f)
		if err != nil {
			slog.Error("fatal", "err", err)
			os.Exit(1)
		}

		if err := frags.ExtractAndRunScripts(config.runRoot); err != nil {
			slog.Error("fatal", "err", err)
			os.Exit(1)
		}

		return nil
	}

	def := builder.NewBuildVmDefinition(
		dir,
		nil, nil,
		config.Output,
		config.cpuCores, config.memorySize, arch,
		config.storageSize,
		"ssh", config.debug,
	)

	if config.Output != "" {
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

		out, err := os.Create(path.Base(config.Output))
		if err != nil {
			return err
		}
		defer out.Close()

		if _, err := io.Copy(out, fh); err != nil {
			return err
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

		if currentConfig.runContainer {
			if os.Getuid() == 0 {
				tmpDir, err := os.MkdirTemp(os.TempDir(), "tinyrange_rootfs_*")
				if err != nil {
					return err
				}
				defer os.RemoveAll(tmpDir)

				if err := common.MountTempFilesystem(tmpDir); err != nil {
					return err
				}

				currentConfig.runRoot = tmpDir
			} else {
				return common.EscalateToRoot()
			}
		}

		if currentConfig.runRoot != "" {
			uid := os.Getuid()
			if uid != 0 {
				return fmt.Errorf("run-root needs to run as UID 0")
			}

			ents, err := os.ReadDir(currentConfig.runRoot)
			if err != nil {
				return err
			}

			if len(ents) != 0 {
				return fmt.Errorf("run-root target is not empty")
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
	loginCmd.PersistentFlags().BoolVar(&currentConfig.NoScripts, "no-scripts", false, "Disable script execution.")
	loginCmd.PersistentFlags().StringArrayVarP(&currentConfig.Files, "file", "f", []string{}, "Specify local files/URLs to be copied into the virtual machine. URLs will be downloaded to the build directory first.")
	loginCmd.PersistentFlags().StringArrayVarP(&currentConfig.Archives, "archive", "a", []string{}, "Specify archives to be copied into the virtual machine. A copy will be made in the build directory.")
	loginCmd.PersistentFlags().StringVarP(&currentConfig.Output, "output", "o", "", "Write the specified file from the guest to the host.")
	loginCmd.PersistentFlags().StringArrayVarP(&currentConfig.Environment, "environment", "e", []string{}, "Add environment variables to the VM.")
	loginCmd.PersistentFlags().StringVar(&currentConfig.Architecture, "arch", "", "Override the CPU architecture of the machine. This will use emulation with a performance hit.")

	// private flags (need to set on command line)
	loginCmd.PersistentFlags().IntVar(&currentConfig.cpuCores, "cpu", 1, "The number of CPU cores to allocate to the virtual machine.")
	loginCmd.PersistentFlags().IntVar(&currentConfig.memorySize, "ram", 1024, "The amount of ram in the virtual machine in megabytes.")
	loginCmd.PersistentFlags().IntVar(&currentConfig.storageSize, "storage", 1024, "The amount of storage to allocate in the virtual machine in megabytes.")
	loginCmd.PersistentFlags().BoolVar(&currentConfig.debug, "debug", false, "Redirect output from the hypervisor to the host. the guest will exit as soon as the VM finishes startup.")
	loginCmd.PersistentFlags().StringVar(&currentConfig.writeRoot, "write-root", "", "Write the root filesystem as a .tar.gz archive.")
	if runtime.GOOS == "linux" {
		if os.Getuid() == 0 {
			// Needs to be running as root.
			loginCmd.PersistentFlags().StringVar(&currentConfig.runRoot, "run-root", "", "Extract the generated root filesystem to a given path and run init scripts. This should only be run in a fresh container filesystem.")
		}
		loginCmd.PersistentFlags().BoolVar(&currentConfig.runContainer, "run-container", false, "use a user namespace to escalate to root privileges so run-root can be used. Also creates a tmpfs for run-root.")
	}
	rootCmd.AddCommand(loginCmd)
}

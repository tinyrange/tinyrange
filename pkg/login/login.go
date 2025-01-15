package login

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/tinyrange/tinyrange/pkg/builder"
	"github.com/tinyrange/tinyrange/pkg/common"
	cfg "github.com/tinyrange/tinyrange/pkg/config"
	"gopkg.in/yaml.v3"
)

func detectArchiveExtractor(base common.BuildDefinition, filename string) (common.BuildDefinition, error) {
	if builder.ReadArchiveSupportsExtracting(filename) {
		return builder.Factory.NewReadArchiveBuildDefinition(base, filename), nil
	} else if strings.HasSuffix(filename, ".archive") {
		return base, nil
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

func parseOciImage(ociImage string) (registry string, image string, tag string, err error) {
	var ok bool

	image, tag, ok = strings.Cut(ociImage, ":")
	if !ok {
		tag = "latest"
	}

	if strings.Contains(image, ".") {
		registry, image, ok = strings.Cut(image, "/")
		if !ok {
			return "", "", "", fmt.Errorf("invalid OCI image format %s", ociImage)
		}
	}

	if registry == "" {
		registry = builder.DEFAULT_REGISTRY
	}

	if registry == "docker.io" {
		registry = builder.DEFAULT_REGISTRY
	}

	if !strings.HasPrefix(registry, "http://") && !strings.HasPrefix(registry, "https://") {
		registry = "https://" + registry
	}

	if registry == builder.DEFAULT_REGISTRY && !strings.Contains(image, "/") {
		image = "library/" + image
	}

	slog.Debug("parsed OCI image", "registry", registry, "image", image, "tag", tag)

	return
}

func toOciArchitecture(arch cfg.CPUArchitecture) (string, error) {
	switch arch {
	case cfg.ArchX8664:
		return "amd64", nil
	case cfg.ArchARM64:
		return "arm64", nil
	case cfg.ArchInvalid:
		return toOciArchitecture(cfg.HostArchitecture)
	default:
		return "", fmt.Errorf("unsupported architecture: %s", arch)
	}
}

var CURRENT_CONFIG_VERSION = 1

type VMSpec struct {
	CpuCores    int `json:"cpu" yaml:"cpu"`
	MemorySize  int `json:"memory" yaml:"memory"`
	StorageSize int `json:"disk" yaml:"disk"`
}

type Config struct {
	Version          int      `json:"version" yaml:"version"`
	Builder          string   `json:"builder" yaml:"builder"`
	OciImage         string   `json:"oci_image,omitempty" yaml:"oci_image,omitempty"`
	Architecture     string   `json:"architecture,omitempty" yaml:"architecture,omitempty"`
	RootArchitecture string   `json:"root_architecture,omitempty" yaml:"root_architecture,omitempty"`
	Commands         []string `json:"commands,omitempty" yaml:"commands,omitempty"`
	ServiceCommands  []string `json:"service_commands,omitempty" yaml:"service_commands,omitempty"`
	Files            []string `json:"files,omitempty" yaml:"files,omitempty"`
	Archives         []string `json:"archives,omitempty" yaml:"archives,omitempty"`
	Output           string   `json:"output,omitempty" yaml:"output,omitempty"`
	Packages         []string `json:"packages,omitempty" yaml:"packages,omitempty"`
	Macros           []string `json:"macros,omitempty" yaml:"macros,omitempty"`
	Environment      []string `json:"environment,omitempty" yaml:"environment,omitempty"`
	NoScripts        bool     `json:"no_scripts,omitempty" yaml:"no_scripts,omitempty"`
	Init             string   `json:"init,omitempty" yaml:"init,omitempty"`
	ForwardPorts     []string `json:"forward_ports,omitempty" yaml:"forward_ports,omitempty"`
	MinSpec          VMSpec   `json:"min_spec,omitempty" yaml:"min_spec,omitempty"`

	// secure configs that have to be set on the command line.
	CpuCores          int      `json:"-" yaml:"-"`
	MemorySize        int      `json:"-" yaml:"-"`
	StorageSize       int      `json:"-" yaml:"-"`
	Debug             bool     `json:"-" yaml:"-"`
	WriteRoot         string   `json:"-" yaml:"-"`
	WriteDocker       string   `json:"-" yaml:"-"`
	ExperimentalFlags []string `json:"-" yaml:"-"`
	Hash              bool     `json:"-" yaml:"-"`
	WebSSH            string   `json:"-" yaml:"-"`
	WriteTemplate     bool     `json:"-" yaml:"-"`
	ReadOnlyMounts    []string `json:"-" yaml:"-"`
	ReadWriteMounts   []string `json:"-" yaml:"-"`

	localConfig bool
	basePath    string
}

// replaceVariables replaces variables like $VAR or ${VAR} in a string with the value of the variable.
func (config *Config) replaceVariables(s string) string {
	return os.Expand(s, func(key string) string {
		for _, env := range config.Environment {
			k, v, ok := strings.Cut(env, "=")
			if !ok {
				continue
			}

			if k == key {
				return v
			}
		}

		return ""
	})
}

func (config *Config) SetBasePath(path string) { config.basePath = path }

func (config *Config) SetLocalConfig() { config.localConfig = true }

func (config *Config) SetVmSpec() {
	if config.CpuCores < config.MinSpec.CpuCores {
		config.CpuCores = config.MinSpec.CpuCores
	}
	if config.MemorySize < config.MinSpec.MemorySize {
		config.MemorySize = config.MinSpec.MemorySize
	}
	if config.StorageSize < config.MinSpec.StorageSize {
		config.StorageSize = config.MinSpec.StorageSize
	}
}

func (config *Config) resolvePath(filename string) (string, error) {
	if filepath.IsAbs(filename) {
		return filename, nil
	}

	return filepath.Join(config.basePath, filename), nil
}

func (config *Config) parseInclusion(db common.PackageDatabase, inclusion string) (common.Directive, error) {
	if !strings.HasSuffix(inclusion, ".yaml") {
		return nil, nil
	}

	subConfig := Config{Version: CURRENT_CONFIG_VERSION}

	f, err := os.Open(inclusion)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	dec := yaml.NewDecoder(f)

	if err := dec.Decode(&subConfig); err != nil {
		return nil, err
	}

	if subConfig.Output == "" {
		return nil, fmt.Errorf("inclusions must have an output file declared")
	}

	directives, interaction, err := subConfig.getDirectives(db)
	if err != nil {
		return nil, err
	}

	arch, err := cfg.ArchitectureFromString(subConfig.Architecture)
	if err != nil {
		return nil, err
	}

	vmArch := arch

	if subConfig.RootArchitecture != "" {
		arch, err = cfg.ArchitectureFromString(subConfig.RootArchitecture)
		if err != nil {
			return nil, err
		}
	}

	if config.Init != "" {
		interaction = "init," + config.Init
	}

	subConfig.SetVmSpec()

	def := builder.Factory.NewBuildVmDefinition(
		directives,
		nil, nil,
		subConfig.replaceVariables(subConfig.Output),
		subConfig.CpuCores, subConfig.MemorySize, vmArch, arch,
		subConfig.StorageSize,
		interaction, subConfig.Debug,
	)

	return common.DirectiveAddFile{
		Filename:   subConfig.replaceVariables(subConfig.Output),
		Definition: def,
	}, nil
}

func (config *Config) getDirectives(db common.PackageDatabase) ([]common.Directive, string, error) {
	var directives []common.Directive

	if config.Builder == "" {
		return nil, "", fmt.Errorf("please specify a builder")
	}

	var tags common.TagList

	tags = append(tags, "level3", "defaults")

	if slices.Contains(common.GetExperimentalFlags(), "slowBoot") {
		tags = append(tags, "slowBoot")
	}

	if config.NoScripts || config.WriteRoot != "" {
		tags = append(tags, "noScripts")
	}

	arch, err := cfg.ArchitectureFromString(config.Architecture)
	if err != nil {
		return nil, "", err
	}

	vmArch := arch

	if config.RootArchitecture != "" {
		arch, err = cfg.ArchitectureFromString(config.RootArchitecture)
		if err != nil {
			return nil, "", err
		}
	}

	for _, filename := range config.Files {
		filename = config.replaceVariables(filename)

		if strings.HasPrefix(filename, "http://") || strings.HasPrefix(filename, "https://") {
			parsed, err := url.Parse(filename)
			if err != nil {
				return nil, "", err
			}

			base := path.Base(parsed.Path)

			directives = append(directives, common.DirectiveAddFile{
				Definition: builder.Factory.NewFetchHttpBuildDefinition(filename, 0, nil),
				Filename:   path.Join("/root", base),
			})
		} else {
			if !config.localConfig {
				return nil, "", fmt.Errorf("remote configs can't include local files")
			}

			filePath, err := config.resolvePath(filename)
			if err != nil {
				return nil, "", err
			}

			directives = append(directives, common.DirectiveLocalFile{
				HostFilename: filePath,
				Filename:     path.Join("/root", filepath.Base(filePath)),
			})
		}
	}

	for _, filename := range config.Archives {
		filename = config.replaceVariables(filename)

		var def common.BuildDefinition

		filename, target, ok := strings.Cut(filename, ",")

		if !ok {
			if strings.HasSuffix(filename, ".archive") {
				target = "/"
			} else {
				target = "/root"
			}
		}

		if strings.HasPrefix(filename, "http://") || strings.HasPrefix(filename, "https://") {
			def = builder.Factory.NewFetchHttpBuildDefinition(filename, 0, nil)

			parsed, err := url.Parse(filename)
			if err != nil {
				return nil, "", err
			}

			filename = parsed.Path
		} else {
			if !config.localConfig {
				return nil, "", fmt.Errorf("remote configs can't include local files")
			}

			filePath, err := config.resolvePath(filename)
			if err != nil {
				return nil, "", err
			}

			hash, err := sha256HashFromFile(filePath)
			if err != nil {
				return nil, "", err
			}

			def = builder.Factory.NewConstantHashDefinition(hash, func() (io.ReadCloser, error) {
				return os.Open(filePath)
			})
		}

		ark, err := detectArchiveExtractor(def, filename)
		if err != nil {
			return nil, "", err
		}

		directives = append(directives, common.DirectiveArchive{Definition: ark, Target: target})
	}

	var pkgs []common.PackageQuery

	for _, arg := range config.Packages {
		q, err := common.ParsePackageQuery(arg)
		if err != nil {
			return nil, "", err
		}

		pkgs = append(pkgs, q)
	}

	macroCtx := db.NewMacroContext()

	if arch == cfg.ArchInvalid {
		macroCtx.AddVariable("arch", string(cfg.HostArchitecture))
	} else {
		macroCtx.AddVariable("arch", string(arch))
	}

	if vmArch == cfg.ArchInvalid {
		macroCtx.AddVariable("guest_arch", string(cfg.HostArchitecture))
	} else {
		macroCtx.AddVariable("guest_arch", string(vmArch))
	}

	var planDirective builder.PlanDefinition
	if config.OciImage != "" {
		if strings.HasPrefix(config.OciImage, "./") {
			// assume this is a local archive which needs to be imported.
			if !config.localConfig {
				return nil, "", fmt.Errorf("remote configs can't include local files")
			}

			filePath, err := config.resolvePath(config.OciImage)
			if err != nil {
				return nil, "", err
			}

			hash, err := sha256HashFromFile(filePath)
			if err != nil {
				return nil, "", err
			}

			def := builder.Factory.NewConstantHashDefinition(hash, func() (io.ReadCloser, error) {
				return os.Open(filePath)
			})

			readArchiveDef := builder.Factory.NewReadArchiveBuildDefinition(def, filePath)

			ociDef := builder.Factory.NewReadOCIImageDefinition(readArchiveDef)

			directives = append(directives, ociDef)
		} else {
			registry, image, tag, err := parseOciImage(config.OciImage)
			if err != nil {
				return nil, "", err
			}

			ociArch, err := toOciArchitecture(arch)
			if err != nil {
				return nil, "", err
			}

			ociDef := builder.Factory.NewFetchOCIImageDefinition(registry, image, tag, ociArch)

			directives = append(directives, ociDef)
		}
	} else {
		planDirective, err = builder.Factory.NewPlanDefinition(config.Builder, arch, pkgs, tags)
		if err != nil {
			return nil, "", err
		}

		macroCtx.AddBuilder("default", planDirective)
	}

	for _, macro := range config.Macros {
		vm, err := config.parseInclusion(db, macro)
		if err != nil {
			return nil, "", err
		}

		if vm != nil {
			directives = append(directives, vm)
		} else {
			m, err := db.GetMacroByShorthand(macroCtx, macro, config.localConfig)
			if err != nil {
				return nil, "", err
			}

			def, err := m.Call(macroCtx)
			if err != nil {
				return nil, "", err
			}

			if star, ok := def.(*common.StarDirective); ok {
				def = star.Directive
			}

			if dir, ok := def.(common.Directive); ok {
				directives = append(directives, dir)
			} else {
				return nil, "", fmt.Errorf("handling of macro def %T not implemented", def)
			}
		}
	}

	for _, mount := range config.ReadOnlyMounts {
		// Mounts are private so we don't need to check if they're remote.

		p, err := filepath.Abs(mount)
		if err != nil {
			return nil, "", err
		}

		directives = append(directives, common.DirectiveMountHostDirectory{HostDirectory: p})
	}

	for _, mount := range config.ReadWriteMounts {
		// Mounts are private so we don't need to check if they're remote.

		p, err := filepath.Abs(mount)
		if err != nil {
			return nil, "", err
		}

		directives = append(directives, common.DirectiveMountHostDirectory{HostDirectory: p, Writable: true})
	}

	if (len(config.ReadOnlyMounts) > 0 || len(config.ReadWriteMounts) > 0) &&
		strings.HasPrefix(config.Builder, "alpine@") && config.OciImage == "" {
		directives = append(directives, common.DirectiveAddPackage{Name: common.PackageQuery{Name: "sshfs"}})
		directives = append(directives, common.DirectiveRunCommand{Command: strings.Join([]string{
			"mkdir /share",
			"mkdir /root/.ssh",
			"ssh-keyscan host.internal > /root/.ssh/known_hosts 2> /dev/null",
			"echo 'password' | sshfs -o password_stdin host.internal:/ /share",
		}, "\n")})
	}

	if config.WriteRoot == "" && config.WriteDocker == "" {
		for _, cmd := range config.ServiceCommands {
			directives = append(directives, common.DirectiveStartServiceCommand{Command: cmd})
		}

		if len(config.Commands) == 0 && config.Init == "" {
			directives = append(directives, common.DirectiveRunCommand{Command: "interactive"})
		} else {
			for _, cmd := range config.Commands {
				directives = append(directives, common.DirectiveRunCommand{Command: cmd})
			}
		}
	}

	if len(config.Environment) > 0 {
		directives = append(directives, common.DirectiveEnvironment{Variables: config.Environment})
	}

	for _, port := range config.ForwardPorts {
		portNum, err := strconv.Atoi(port)
		if err != nil {
			return nil, "", err
		}

		directives = append(directives, common.DirectiveExportPort{Name: "forward", Port: portNum})
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
		return nil, "", err
	}

	if planDirective != nil {
		directives = append([]common.Directive{planDirective}, directives...)
	}

	return directives, interaction, nil
}

func (config *Config) MakeTemplate(db common.PackageDatabase) (string, error) {
	if config.Version > CURRENT_CONFIG_VERSION {
		return "", fmt.Errorf("attempt to run config version %d on TinyRange version %d", config.Version, CURRENT_CONFIG_VERSION)
	}

	directives, interaction, err := config.getDirectives(db)
	if err != nil {
		return "", err
	}

	arch, err := cfg.ArchitectureFromString(config.Architecture)
	if err != nil {
		return "", err
	}

	if config.RootArchitecture != "" {
		arch, err = cfg.ArchitectureFromString(config.RootArchitecture)
		if err != nil {
			return "", err
		}
	}

	vmArch := arch

	if config.Init != "" {
		interaction = "init," + config.Init
	}

	if config.WebSSH != "" {
		interaction = "webssh," + config.WebSSH
	}

	config.SetVmSpec()

	def := builder.Factory.NewBuildVmDefinition(
		directives,
		nil, nil,
		config.replaceVariables(config.Output),
		config.CpuCores, config.MemorySize,
		vmArch, arch,
		config.StorageSize,
		interaction, config.Debug,
	)

	def.SetBuildTemplateMode()

	_, err = db.Builder().Build(def, common.BuildOptions{AlwaysRebuild: true})
	if built, ok := err.(common.ErrTemplateBuilt); ok {
		return string(built), nil
	} else if err != nil {
		return "", err
	} else {
		return "", fmt.Errorf("failed to write template output")
	}
}

func (config *Config) Run(db common.PackageDatabase) error {
	if config.Version > CURRENT_CONFIG_VERSION {
		return fmt.Errorf("attempt to run config version %d on TinyRange version %d", config.Version, CURRENT_CONFIG_VERSION)
	}

	if config.Builder == "list" {
		for name, builder := range db.GetContainerBuilders() {
			fmt.Printf(" - %s - %s\n", name, builder.DisplayName())
		}

		return nil
	}

	directives, interaction, err := config.getDirectives(db)
	if err != nil {
		return err
	}

	arch, err := cfg.ArchitectureFromString(config.Architecture)
	if err != nil {
		return err
	}

	vmArch := arch

	if config.RootArchitecture != "" {
		arch, err = cfg.ArchitectureFromString(config.RootArchitecture)
		if err != nil {
			return err
		}
	}

	if config.WriteRoot != "" {
		directives = append(directives, common.DirectiveBuiltin{Name: "init", Architecture: string(arch), GuestFilename: "init"})

		def := builder.Factory.NewBuildFsDefinition(directives, "tar")

		art, err := db.Builder().Build(def, common.BuildOptions{})
		if err != nil {
			slog.Error("fatal", "err", err)
			os.Exit(1)
		}

		f, err := art.Default()
		if err != nil {
			return err
		}

		fh, err := f.Open()
		if err != nil {
			return err
		}
		defer fh.Close()

		out, err := os.Create(path.Base(config.WriteRoot))
		if err != nil {
			return err
		}
		defer out.Close()

		if _, err := io.Copy(out, fh); err != nil {
			return err
		}

		return nil
	} else if config.WriteDocker != "" {
		ctx := context.Background()

		apiClient, err := client.NewClientWithOpts(client.FromEnv)
		if err != nil {
			slog.Error("fatal", "err", err)
			os.Exit(1)
		}
		defer apiClient.Close()

		directives = append(directives, common.DirectiveBuiltin{Name: "init", Architecture: string(arch), GuestFilename: "init"})

		def := builder.Factory.NewBuildFsDefinition(directives, "tar")

		art, err := db.Builder().Build(def, common.BuildOptions{})
		if err != nil {
			slog.Error("fatal", "err", err)
			os.Exit(1)
		}

		f, err := art.Default()
		if err != nil {
			return err
		}

		buildCtxOut, buildCtxIn := io.Pipe()

		go func() {
			err := func() error {
				defer buildCtxIn.Close()

				w := tar.NewWriter(buildCtxIn)

				fh, err := f.Open()
				if err != nil {
					return err
				}
				defer fh.Close()

				info, err := f.Stat()
				if err != nil {
					return err
				}

				if err := w.WriteHeader(&tar.Header{
					Typeflag: tar.TypeReg,
					Name:     "rootfs.tar",
					Size:     info.Size(),
					Mode:     int64(info.Mode()),
				}); err != nil {
					return err
				}

				if _, err := io.Copy(w, fh); err != nil {
					return err
				}

				dockerfile := "FROM scratch\nADD rootfs.tar .\nRUN /init -run-basic-scripts /init.commands.json"

				if err := w.WriteHeader(&tar.Header{
					Typeflag: tar.TypeReg,
					Name:     "Dockerfile",
					Size:     int64(len(dockerfile)),
					Mode:     int64(os.ModePerm),
				}); err != nil {
					return err
				}

				if _, err := w.Write([]byte(dockerfile)); err != nil {
					return err
				}

				return nil
			}()
			if err != nil {
				slog.Error("fatal", "err", err)
				os.Exit(1)
			}
		}()

		resp, err := apiClient.ImageBuild(ctx, buildCtxOut, types.ImageBuildOptions{
			Tags:       []string{config.WriteDocker},
			Dockerfile: "Dockerfile",
		})
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		dec := json.NewDecoder(resp.Body)

		var item map[string]any

		for {
			item = nil

			err := dec.Decode(&item)
			if err == io.EOF {
				break
			} else if err != nil {
				return err
			}

			if stream, ok := item["stream"]; ok {
				fmt.Fprintf(os.Stdout, "%s", stream)
			} else {
				slog.Info("", "item", item)
			}
		}

		return nil
	} else {
		if config.Init != "" {
			interaction = "init," + config.Init
		}

		if config.WebSSH != "" {
			interaction = "webssh," + config.WebSSH
		}

		var kernel common.BuildDefinition
		var initramfs common.BuildDefinition

		directives, err = common.FlattenDirectives(directives, common.SpecialDirectiveHandlers{
			Kernel: func(dir common.DirectiveKernel) error {
				kernel = dir.Kernel
				initramfs = dir.Initramfs

				return nil
			},
		})
		if err != nil {
			return err
		}

		config.SetVmSpec()

		def := builder.Factory.NewBuildVmDefinition(
			directives,
			kernel, initramfs,
			config.replaceVariables(config.Output),
			config.CpuCores, config.MemorySize,
			vmArch, arch,
			config.StorageSize,
			interaction, config.Debug,
		)

		if config.WriteTemplate {
			def.SetBuildTemplateMode()

			_, err := db.Builder().Build(def, common.BuildOptions{AlwaysRebuild: true})
			if built, ok := err.(common.ErrTemplateBuilt); ok {
				fmt.Printf("%s\n", string(built))

				return nil
			} else if err != nil {
				return err
			} else {
				return fmt.Errorf("failed to write template output")
			}
		} else if config.Output != "" {
			opts := common.BuildOptions{}
			if len(config.Commands) == 0 {
				// Always rebuild if this is interactive.
				opts.AlwaysRebuild = true
			}

			art, err := db.Builder().Build(def, opts)
			if err != nil {
				slog.Error("fatal", "err", err)
				os.Exit(1)
			}

			f, err := art.Default()
			if err != nil {
				return err
			}

			fh, err := f.Open()
			if err != nil {
				return err
			}
			defer fh.Close()

			output := config.replaceVariables(config.Output)

			out, err := os.Create(path.Base(output))
			if err != nil {
				return err
			}
			defer out.Close()

			if _, err := io.Copy(out, fh); err != nil {
				return err
			}

			if config.Hash {
				slog.Info("wrote output", "filename", path.Base(output))
			}

			return nil
		} else {
			if _, err := db.Builder().Build(def, common.BuildOptions{
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
}

package login

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/tinyrange/tinyrange/pkg/builder"
	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/config"
	cfg "github.com/tinyrange/tinyrange/pkg/config"
	"github.com/tinyrange/tinyrange/pkg/feature"
	"github.com/tinyrange/tinyrange/pkg/log"
	"github.com/tinyrange/tinyrange/pkg/path"
)

func detectArchiveExtractor(base common.BuildDefinition, filename string) (common.BuildDefinition, error) {
	if _, ok := builder.ReadArchiveSupportsExtracting(filename, false); ok {
		return builder.Factory.NewReadArchiveBuildDefinition(base, filename, 0), nil
	} else if strings.HasSuffix(filename, ".archive") {
		return base, nil
	} else {
		return nil, fmt.Errorf("no extractor for %s", filename)
	}
}

func parseMount(mount string, writable bool, port int) (common.DirectiveMountHostDirectory, error) {
	if strings.Contains(mount, ":") {
		var (
			host  string
			guest string
		)

		parts := strings.Split(mount, ":")
		if len(parts) == 2 {
			host, guest = parts[0], parts[1]
		} else if len(parts) == 3 && runtime.GOOS == "windows" {
			// assume that the user wrote something like C:/host:/guest
			host, guest = parts[0]+":"+parts[1], parts[2]
		} else {
			return common.DirectiveMountHostDirectory{}, fmt.Errorf("invalid mount %s", mount)
		}

		hostPath, err := path.Native.Abs(host)
		if err != nil {
			return common.DirectiveMountHostDirectory{}, err
		}

		return common.DirectiveMountHostDirectory{
			HostDirectory:  hostPath,
			GuestDirectory: guest,
			Port:           port,
			Writable:       writable,
		}, nil
	} else if strings.Contains(mount, ",") {
		return common.DirectiveMountHostDirectory{}, fmt.Errorf("invalid mount %s (syntax host:guest)", mount)
	} else {
		mountPath, err := path.Native.Abs(mount)
		if err != nil {
			return common.DirectiveMountHostDirectory{}, err
		}

		guest := path.Unix.Join("/share", path.Native.Base(mountPath))

		return common.DirectiveMountHostDirectory{
			HostDirectory:  mountPath,
			GuestDirectory: guest,
			Port:           port,
			Writable:       writable,
		}, nil
	}
}

func parseVolumeSize(token string) (uint64, error) {
	multiplier := uint64(1)

	token = strings.ToLower(token)

	if strings.HasSuffix(token, "g") {
		multiplier = 1024
		token = strings.TrimSuffix(token, "g")
	} else if strings.HasSuffix(token, "m") {
		multiplier = 1
		token = strings.TrimSuffix(token, "m")
	} else if strings.HasSuffix(token, "t") {
		multiplier = 1024 * 1024
		token = strings.TrimSuffix(token, "t")
	}

	size, err := strconv.ParseUint(token, 0, 64)
	if err != nil {
		return 0, err
	}

	return size * multiplier, nil
}

func parseVolume(volume string) (common.DirectiveAddVolume, error) {
	// name, size, guestPath split by ,
	tokens := strings.Split(volume, ",")
	if len(tokens) >= 3 {
		name := tokens[0]
		minSize, err := parseVolumeSize(tokens[1])
		if err != nil {
			return common.DirectiveAddVolume{}, err
		}
		guestPath := tokens[2]

		dir := common.DirectiveAddVolume{
			VolumeName:    name,
			MinimumSizeMB: minSize,
			GuestPath:     guestPath,
		}

		if len(tokens) == 4 && tokens[3] == "persist" {
			dir.Persist = true
		}

		return dir, nil
	} else {
		return common.DirectiveAddVolume{}, fmt.Errorf("invalid volume %s", volume)
	}
}

var CURRENT_CONFIG_VERSION = 1

type VMSpec struct {
	CpuCores    int `json:"cpu" yaml:"cpu"`
	MemorySize  int `json:"memory" yaml:"memory"`
	StorageSize int `json:"disk" yaml:"disk"`
}

type Config struct {
	Version          int               `json:"version" yaml:"version"`
	Builder          string            `json:"builder" yaml:"builder"`
	OciImage         string            `json:"oci_image,omitempty" yaml:"oci_image,omitempty"`
	Architecture     string            `json:"architecture,omitempty" yaml:"architecture,omitempty"`
	RootArchitecture string            `json:"root_architecture,omitempty" yaml:"root_architecture,omitempty"`
	Commands         []string          `json:"commands,omitempty" yaml:"commands,omitempty"`
	ServiceCommands  []string          `json:"service_commands,omitempty" yaml:"service_commands,omitempty"`
	Layers           []string          `json:"layers,omitempty" yaml:"layers,omitempty"`
	Files            []string          `json:"files,omitempty" yaml:"files,omitempty"`
	FileContents     map[string]string `json:"file_contents,omitempty" yaml:"file_contents,omitempty"`
	Archives         []string          `json:"archives,omitempty" yaml:"archives,omitempty"`
	Output           string            `json:"output,omitempty" yaml:"output,omitempty"`
	Packages         []string          `json:"packages,omitempty" yaml:"packages,omitempty"`
	Macros           []string          `json:"macros,omitempty" yaml:"macros,omitempty"`
	Environment      []string          `json:"environment,omitempty" yaml:"environment,omitempty"`
	NoScripts        bool              `json:"no_scripts,omitempty" yaml:"no_scripts,omitempty"`
	Init             string            `json:"init,omitempty" yaml:"init,omitempty"`
	ForwardPorts     []string          `json:"forward_ports,omitempty" yaml:"forward_ports,omitempty"`
	Volumes          []string          `json:"volumes,omitempty" yaml:"volumes,omitempty"`
	AutoScale        bool              `json:"auto_scale,omitempty" yaml:"auto_scale,omitempty"`
	MinSpec          VMSpec            `json:"min_spec,omitempty" yaml:"min_spec,omitempty"`

	// secure configs that have to be set on the command line.
	CpuCores        int      `json:"-" yaml:"-"`
	MemorySize      int      `json:"-" yaml:"-"`
	StorageSize     int      `json:"-" yaml:"-"`
	Debug           bool     `json:"-" yaml:"-"`
	WriteRoot       string   `json:"-" yaml:"-"`
	WebSSH          string   `json:"-" yaml:"-"`
	WriteTemplate   bool     `json:"-" yaml:"-"`
	ReadOnlyMounts  []string `json:"-" yaml:"-"`
	ReadWriteMounts []string `json:"-" yaml:"-"`

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
	if path.Native.IsAbs(filename) {
		return filename, nil
	}

	return path.Native.Join(config.basePath, filename), nil
}

func (config *Config) writeRoot(db common.PackageDatabase, directives []common.Directive, arch config.CPUArchitecture) error {
	directives = append(directives, common.DirectiveBuiltin{
		Name:          "init",
		Architecture:  string(arch),
		GuestFilename: "init",
	})

	def := builder.Factory.NewBuildFsDefinition(directives, "tar")

	art, err := db.Builder().Build(def, common.BuildOptions{})
	if err != nil {
		log.Error("fatal", "err", err)
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

	out, err := os.Create(path.Unix.Base(config.WriteRoot))
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, fh); err != nil {
		return err
	}

	return nil
}

func (config *Config) addFile(filename string) (common.Directive, error) {
	filename = config.replaceVariables(filename)

	if strings.HasPrefix(filename, "http://") || strings.HasPrefix(filename, "https://") {
		parsed, err := url.Parse(filename)
		if err != nil {
			return nil, err
		}

		target := path.Unix.Join("/root", path.Native.Base(parsed.Path))

		if strings.Contains(parsed.Path, ":") {
			target = strings.SplitN(parsed.Path, ":", 2)[1]
			// remove the target from the filename
			filename = filename[:len(filename)-len(target)-1]
		}

		return common.DirectiveAddFile{
			Definition: builder.Factory.NewFetchHttpBuildDefinition(filename, 0, nil),
			Filename:   target,
		}, nil
	} else {
		if !config.localConfig {
			return nil, fmt.Errorf("remote configs can't include local files")
		}

		target := ""

		if strings.Contains(filename, ":") {
			target = strings.SplitN(filename, ":", 2)[1]
			// remove the target from the filename
			filename = filename[:len(filename)-len(target)-1]
		}

		filePath, err := config.resolvePath(filename)
		if err != nil {
			return nil, err
		}

		if target == "" {
			target = path.Unix.Join("/root", path.Native.Base(filePath))
		}

		stat, err := os.Stat(filePath)
		if err != nil {
			return nil, err
		}

		if stat.IsDir() {
			return common.DirectiveLocalDirectory{
				HostDirectory:  filePath,
				GuestDirectory: target,
			}, nil
		} else {
			return common.DirectiveLocalFile{
				HostFilename: filePath,
				Filename:     target,
			}, nil
		}
	}
}

func (config *Config) addArchive(filename string) (common.Directive, error) {
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
			return nil, err
		}

		filename = parsed.Path
	} else {
		if !config.localConfig {
			return nil, fmt.Errorf("remote configs can't include local files")
		}

		filePath, err := config.resolvePath(filename)
		if err != nil {
			return nil, err
		}

		hash, err := common.Sha256HashFromFile(filePath)
		if err != nil {
			return nil, err
		}

		def = builder.Factory.NewConstantHashDefinition(hash, func() (io.ReadCloser, error) {
			return os.Open(filePath)
		})
	}

	ark, err := detectArchiveExtractor(def, filename)
	if err != nil {
		return nil, err
	}

	return common.DirectiveArchive{Definition: ark, Target: target}, nil
}

func (config *Config) addOCIImage(image string, arch config.CPUArchitecture) (common.Directive, error) {
	if strings.HasPrefix(image, "./") {
		// assume this is a local archive which needs to be imported.
		if !config.localConfig {
			return nil, fmt.Errorf("remote configs can't include local files")
		}

		filePath, err := config.resolvePath(image)
		if err != nil {
			return nil, err
		}

		hash, err := common.Sha256HashFromFile(filePath)
		if err != nil {
			return nil, err
		}

		def := builder.Factory.NewConstantHashDefinition(hash, func() (io.ReadCloser, error) {
			return os.Open(filePath)
		})

		readArchiveDef := builder.Factory.NewReadArchiveBuildDefinition(def, filePath, 0)

		ociDef := builder.Factory.NewReadOCIImageDefinition(readArchiveDef)

		return ociDef, nil
	} else {
		registry, image, tag, err := builder.ParseOciImage(image)
		if err != nil {
			return nil, err
		}

		ociArch, err := builder.ToOciArchitecture(arch)
		if err != nil {
			return nil, err
		}

		ociDef := builder.Factory.NewFetchOCIImageDefinition(registry, image, tag, ociArch)

		return ociDef, nil
	}
}

func (config *Config) addMacro(db common.PackageDatabase, macro string, macroCtx common.MacroContext) (common.Directive, error) {
	m, err := db.GetMacroByShorthand(macroCtx, macro, config.localConfig)
	if err != nil {
		return nil, err
	}

	def, err := m.Call(macroCtx)
	if err != nil {
		return nil, err
	}

	if star, ok := def.(*common.StarDirective); ok {
		def = star.Directive
	}

	if dir, ok := def.(common.Directive); ok {
		return dir, nil
	} else {
		return nil, fmt.Errorf("handling of macro def %T not implemented", def)
	}
}

func (config *Config) addLayer(directives []common.Directive,
	vmArch config.CPUArchitecture,
	arch config.CPUArchitecture,
	layer string,
) (common.Directive, error) {
	vmDef, err := config.makeBuildVMDefinition(
		append(directives, common.DirectiveRunCommand{Command: layer}),
		vmArch, arch,
		"/init/changed.archive",
		"ssh",
	)
	if err != nil {
		return nil, err
	}

	return vmDef, nil
}

func (config *Config) makeBuildVMDefinition(
	directives []common.Directive,
	vmArch config.CPUArchitecture,
	arch config.CPUArchitecture,
	outputName string,
	interaction string,
) (common.BuildVmDefinition, error) {
	var kernel common.BuildDefinition
	var initramfs common.BuildDefinition

	directives, err := common.FlattenDirectives(directives, common.SpecialDirectiveHandlers{
		Kernel: func(dir common.DirectiveKernel) error {
			if dir.Kernel != nil {
				kernel = dir.Kernel
			}
			if dir.Initramfs != nil {
				initramfs = dir.Initramfs
			}

			return nil
		},
	})
	if err != nil {
		return nil, err
	}

	config.SetVmSpec()

	return builder.Factory.NewBuildVmDefinition(
		directives,
		kernel, initramfs,
		outputName,
		config.CpuCores, config.MemorySize, config.AutoScale,
		vmArch, arch,
		config.StorageSize,
		interaction, config.Debug,
	), nil
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

	var directives []common.Directive

	if config.Builder == "" {
		return fmt.Errorf("please specify a builder")
	}

	var tags common.TagList

	tags = append(tags, "level3", "defaults")

	if feature.HasFeature(feature.FeatureSlowBoot) {
		tags = append(tags, "slowBoot")
	}

	if config.NoScripts || config.WriteRoot != "" {
		tags = append(tags, "noScripts")
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

	for _, filename := range config.Files {
		dir, err := config.addFile(filename)
		if err != nil {
			return err
		}

		directives = append(directives, dir)
	}

	// Add raw file contents.
	for guestFilename, content := range config.FileContents {
		directives = append(directives, common.DirectiveAddFile{
			Filename: guestFilename,
			Contents: []byte(content),
		})
	}

	for _, filename := range config.Archives {
		dir, err := config.addArchive(filename)
		if err != nil {
			return err
		}

		directives = append(directives, dir)
	}

	var pkgs []common.PackageQuery

	for _, arg := range config.Packages {
		q, err := common.ParsePackageQuery(arg)
		if err != nil {
			return err
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

	var planDirective common.PlanDefinition
	if config.OciImage != "" {
		def, err := config.addOCIImage(config.OciImage, arch)
		if err != nil {
			return err
		}

		directives = append(directives, def)
	} else {
		planDirective, err = builder.Factory.NewPlanDefinition(config.Builder, arch, pkgs, tags)
		if err != nil {
			return err
		}

		macroCtx.AddBuilder("default", planDirective)
	}

	for _, macro := range config.Macros {
		def, err := config.addMacro(db, macro, macroCtx)
		if err != nil {
			return err
		}

		directives = append(directives, def)
	}

	var mountDirectives []common.DirectiveMountHostDirectory
	var mountPort = 4000

	for _, mount := range config.ReadOnlyMounts {
		// Mounts are private so we don't need to check if they're remote.

		parsed, err := parseMount(mount, false, mountPort)
		if err != nil {
			return err
		}

		mountPort += 1

		directives = append(directives, parsed)
		mountDirectives = append(mountDirectives, parsed)
	}

	for _, mount := range config.ReadWriteMounts {
		// Mounts are private so we don't need to check if they're remote.

		parsed, err := parseMount(mount, true, mountPort)
		if err != nil {
			return err
		}

		mountPort += 1

		directives = append(directives, parsed)
		mountDirectives = append(mountDirectives, parsed)
	}

	for _, volume := range config.Volumes {
		dir, err := parseVolume(volume)
		if err != nil {
			return err
		}

		directives = append(directives, dir)
	}

	if len(mountDirectives) > 0 {
		if feature.HasFeature(feature.Feature9P) {
			// Check that all the directories are accessible.
			for _, mount := range mountDirectives {
				if _, err := os.Stat(mount.HostDirectory); err != nil {
					return fmt.Errorf("mount %s is not accessible: %w", mount.HostDirectory, err)
				}
			}

			scriptLines := []string{
				"def main():",
			}

			for _, mount := range mountDirectives {
				scriptLines = append(scriptLines, fmt.Sprintf(
					"  mount('9p', '10.42.0.1', '%s', options='trans=tcp,version=9p2000.L,port=%d', ensure_path=True)",
					mount.GuestDirectory, mount.Port,
				))
			}

			directives = append(directives, common.DirectiveRunStarlarkScript{
				Script: strings.Join(scriptLines, "\n"),
			})
		} else {
			if strings.HasPrefix(config.Builder, "alpine@") && config.OciImage == "" {
				directives = append(directives, common.DirectiveAddPackage{Name: common.PackageQuery{Name: "sshfs"}})
				directives = append(directives, common.DirectiveRunCommand{Command: strings.Join([]string{
					"mkdir /share",
					"mkdir /root/.ssh",
					"ssh-keyscan host.internal > /root/.ssh/known_hosts 2> /dev/null",
					"echo 'password' | sshfs -o password_stdin host.internal:/ /share",
				}, "\n")})
			} else {
				log.Warn("mounts cannot be automatically configured for this builder")
			}
		}
	}

	if len(config.Environment) > 0 {
		directives = append(directives, common.DirectiveEnvironment{Variables: config.Environment})
	}

	forwardedPorts := make(map[int]struct{})
	for _, port := range config.ForwardPorts {
		portNum, err := strconv.Atoi(port)
		if err != nil {
			return err
		}

		if _, ok := forwardedPorts[portNum]; ok {
			continue
		}
		forwardedPorts[portNum] = struct{}{}

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
		return err
	}

	if planDirective != nil {
		directives = append([]common.Directive{planDirective}, directives...)
	}

	for _, layer := range config.Layers {
		def, err := config.addLayer(directives, vmArch, arch, layer)
		if err != nil {
			return err
		}

		directives = append(directives, def)
	}

	if config.WriteRoot == "" {
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

	if config.RootArchitecture != "" {
		arch, err = cfg.ArchitectureFromString(config.RootArchitecture)
		if err != nil {
			return err
		}
	}

	if config.WriteRoot != "" {
		return config.writeRoot(db, directives, arch)
	}

	if config.Init != "" {
		interaction = "init," + config.Init
	}

	if config.WebSSH != "" {
		interaction = "webssh," + config.WebSSH
	}

	outputName := config.replaceVariables(config.Output)

	def, err := config.makeBuildVMDefinition(directives, vmArch, arch, outputName, interaction)
	if err != nil {
		return err
	}

	if config.WriteTemplate {
		def.SetBuildTemplateMode()

		_, err := db.Builder().Build(def, common.BuildOptions{AlwaysRebuild: true})
		var built common.ErrTemplateBuilt
		if errors.As(err, &built) {
			fmt.Printf("%s\n", string(built))

			return nil
		} else if err != nil {
			return err
		} else {
			return fmt.Errorf("failed to write template output")
		}
	}

	if config.Output != "" {
		opts := common.BuildOptions{}
		if len(config.Commands) == 0 {
			// Always rebuild if this is interactive.
			opts.AlwaysRebuild = true
		}

		art, err := db.Builder().Build(def, opts)
		if err != nil {
			log.Error("fatal", "err", err)
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

		out, err := os.Create(path.Unix.Base(outputName))
		if err != nil {
			return err
		}
		defer out.Close()

		if _, err := io.Copy(out, fh); err != nil {
			return err
		}

		return nil
	}

	if _, err := db.Builder().Build(def, common.BuildOptions{
		AlwaysRebuild: true,
	}); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}

	return nil
}

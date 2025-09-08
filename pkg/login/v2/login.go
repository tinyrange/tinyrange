package v2

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"net/url"
	"os"

	"github.com/tinyrange/tinyrange/pkg/builder"
	"github.com/tinyrange/tinyrange/pkg/common"
	cfg "github.com/tinyrange/tinyrange/pkg/config"
	loginv1 "github.com/tinyrange/tinyrange/pkg/login"
	"github.com/tinyrange/tinyrange/pkg/path"
)

const CURRENT_CONFIG_VERSION = 2

type ByteQuantity string

func (bq ByteQuantity) ToBytes() (int64, error) {
	// Default interpret as megabytes when no suffix provided.
	var multiplier int64 = 1024 * 1024

	s := strings.TrimSpace(string(bq))
	n := len(s)
	if n == 0 {
		return 0, fmt.Errorf("invalid byte quantity: empty string")
	}

	last := s[n-1]
	switch last {
	case 'b', 'B':
		multiplier = 1
		s = s[:n-1]
	case 'k', 'K':
		multiplier = 1024
		s = s[:n-1]
	case 'm', 'M':
		multiplier = 1024 * 1024
		s = s[:n-1]
	case 'g', 'G':
		multiplier = 1024 * 1024 * 1024
		s = s[:n-1]
	case 't', 'T':
		multiplier = 1024 * 1024 * 1024 * 1024
		s = s[:n-1]
	}

	value, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid byte quantity: %w", err)
	}
	return value * multiplier, nil
}

type context struct {
	arch         cfg.CPUArchitecture
	directives   []common.Directive
	autoScale    bool
	storageSizeM int
	workdir      string
	interaction  string
	db           common.PackageDatabase
	basePath     string
	localConfig  bool
}

type directive interface {
	Apply(ctx *context) error
}

type PlanDirective struct {
	Builder  string   `yaml:"builder,omitempty"`
	Packages []string `yaml:"packages,omitempty"`
}

// Apply implements directive.
func (p *PlanDirective) Apply(ctx *context) error {
	if p == nil {
		return fmt.Errorf("plan directive is nil")
	}
	if p.Builder == "" {
		return fmt.Errorf("plan directive requires builder")
	}

	var pkgs []common.PackageQuery
	for _, arg := range p.Packages {
		q, err := common.ParsePackageQuery(arg)
		if err != nil {
			return err
		}
		pkgs = append(pkgs, q)
	}

	// Default tag set similar to v1.
	var tags common.TagList
	tags = append(tags, "level3", "defaults")

	plan, err := builder.Factory.NewPlanDefinition(p.Builder, ctx.arch, pkgs, tags)
	if err != nil {
		return err
	}
	ctx.directives = append(ctx.directives, plan)
	return nil
}

type FromDirective string

// Apply implements directive.
func (f FromDirective) Apply(ctx *context) error {
	if f == "" {
		return fmt.Errorf("from directive is empty")
	}
	// Only handle remote OCI images here.
	registry, image, tag, err := builder.ParseOciImage(string(f))
	if err != nil {
		return err
	}
	ociArch, err := builder.ToOciArchitecture(ctx.arch)
	if err != nil {
		return err
	}
	def := builder.Factory.NewFetchOCIImageDefinition(registry, image, tag, ociArch)
	ctx.directives = append(ctx.directives, def)
	return nil
}

type RunDirective []string

// Apply implements directive.
func (r RunDirective) Apply(ctx *context) error {
	if len(r) == 0 {
		return fmt.Errorf("run directive must contain at least one command")
	}

	var runDirs []common.Directive

	runDirs = append(runDirs, common.DirectiveRunCommand{Command: "%change_tracker"})

	runDirs = append(runDirs, common.DirectiveRunCommand{Command: "%exit_on_failure"})

	for _, cmd := range r {
		cmd = strings.TrimSpace(cmd)
		if cmd == "" {
			return fmt.Errorf("run directive contains empty command")
		}
		runDirs = append(runDirs, common.DirectiveRunCommand{Command: cmd})
	}

	// Snapshot current directives to avoid sharing backing array with ctx.directives;
	// otherwise appending vmDef to ctx.directives could backfill into the layer's
	// directive list and introduce a self-reference.
	base := make([]common.Directive, len(ctx.directives))
	copy(base, ctx.directives)
	// Build a single layer VM that runs all commands in order and outputs a single delta archive.
	vmDef := builder.Factory.NewBuildVmDefinition(
		append(base, runDirs...),
		nil, nil,
		"/init/changed.archive",
		0, 0, ctx.autoScale,
		ctx.arch, ctx.arch,
		ctx.storageSizeM,
		"ssh",
		"",
		false,
	)
	ctx.directives = append(ctx.directives, vmDef)
	return nil
}

type EnvironmentDirective map[string]string

// Apply implements directive.
func (e EnvironmentDirective) Apply(ctx *context) error {
	if e == nil {
		return fmt.Errorf("environment directive is nil")
	}
	var vars []string
	for k, v := range e {
		vars = append(vars, fmt.Sprintf("%s=%s", k, v))
	}
	if len(vars) > 0 {
		ctx.directives = append(ctx.directives, common.DirectiveEnvironment{Variables: vars})
	}
	return nil
}

type ExposeDirective []int

// Apply implements directive.
func (e ExposeDirective) Apply(ctx *context) error {
	if len(e) == 0 {
		return fmt.Errorf("expose directive must contain at least one port")
	}
	for _, p := range e {
		if p <= 0 {
			return fmt.Errorf("invalid port: %d", p)
		}
		ctx.directives = append(ctx.directives, common.DirectiveExportPort{Name: "forward", ListenAddress: "localhost", Port: p})
	}
	return nil
}

type ServiceDirective []string

// Apply implements directive (start background services before entrypoint)
func (s ServiceDirective) Apply(ctx *context) error {
	if len(s) == 0 {
		return fmt.Errorf("service directive must contain at least one command")
	}
	for _, cmd := range s {
		if strings.TrimSpace(cmd) == "" {
			return fmt.Errorf("service directive contains empty command")
		}
		ctx.directives = append(ctx.directives, common.DirectiveStartServiceCommand{Command: cmd})
	}
	return nil
}

type WorkdirDirective string

// Apply implements directive.
func (w WorkdirDirective) Apply(ctx *context) error {
	d := string(w)
	if d == "" {
		return fmt.Errorf("workdir cannot be empty")
	}
	if strings.HasPrefix(d, "/") {
		ctx.workdir = path.Unix.Clean(d)
	} else {
		ctx.workdir = path.Unix.Clean(path.Unix.Join(ctx.workdir, d))
	}
	return nil
}

type FileDirective struct {
	// Exactly one of URL, LocalPath, or Contents should be set.
	URL       string `yaml:"url,omitempty"`
	LocalPath string `yaml:"local_path,omitempty"`
	Contents  string `yaml:"contents,omitempty"`

	Name       string `yaml:"name,omitempty"`
	Executable bool   `yaml:"executable,omitempty"`
}

// Apply implements directive.
func (f *FileDirective) Apply(ctx *context) error {
	if f == nil {
		return fmt.Errorf("file directive is nil")
	}

	if f.URL != "" {
		dest := f.Name
		if dest == "" {
			u, err := url.Parse(f.URL)
			if err != nil {
				return err
			}
			base := path.Unix.Base(u.Path)
			dest = path.Unix.Join(ctx.workdir, base)
		} else if !strings.HasPrefix(dest, "/") {
			dest = path.Unix.Join(ctx.workdir, dest)
		}

		def := builder.Factory.NewFetchHttpBuildDefinition(f.URL, 0, nil)
		ctx.directives = append(ctx.directives, common.DirectiveAddFile{Definition: def, Filename: dest, Executable: f.Executable})
		return nil
	} else if f.LocalPath != "" {
		if !ctx.localConfig {
			return fmt.Errorf("local file references are not allowed in remote configs: %s", f.LocalPath)
		}
		if path.Native.IsAbs(f.LocalPath) {
			return fmt.Errorf("absolute paths are not allowed in local config: %s", f.LocalPath)
		}
		abs := path.Native.Clean(path.Native.Join(ctx.basePath, f.LocalPath))
		if !strings.HasPrefix(abs+string(os.PathSeparator), ctx.basePath+string(os.PathSeparator)) && abs != ctx.basePath {
			return fmt.Errorf("path escapes base directory: %s", f.LocalPath)
		}

		dest := f.Name
		if dest == "" {
			base := path.Native.Base(f.LocalPath)
			dest = path.Unix.Join(ctx.workdir, base)
		} else if !strings.HasPrefix(dest, "/") {
			dest = path.Unix.Join(ctx.workdir, dest)
		}

		hash, err := common.Sha256HashFromFile(abs)
		if err != nil {
			return err
		}
		def := builder.Factory.NewConstantHashDefinition(hash, func() (io.ReadCloser, error) { return os.Open(abs) })
		ctx.directives = append(ctx.directives, common.DirectiveAddFile{Definition: def, Filename: dest, Executable: f.Executable})
		return nil
	} else if f.Contents != "" {
		dest := f.Name
		if dest == "" {
			dest = path.Unix.Join(ctx.workdir, "contents")
		} else if !strings.HasPrefix(dest, "/") {
			dest = path.Unix.Join(ctx.workdir, dest)
		}

		ctx.directives = append(ctx.directives, common.DirectiveAddFile{
			Contents:   []byte(f.Contents),
			Filename:   dest,
			Executable: f.Executable},
		)
		return nil
	} else {
		return fmt.Errorf("file src cannot be empty")
	}
}

type ArchiveDirective struct {
	Src  string `yaml:"src"`
	Dest string `yaml:"dest,omitempty"`
}

// Apply implements directive.
func (a *ArchiveDirective) Apply(ctx *context) error {
	if a == nil {
		return fmt.Errorf("archive directive is nil")
	}
	if a.Src == "" {
		return fmt.Errorf("archive src cannot be empty")
	}

	dest := a.Dest
	if dest == "" {
		dest = ctx.workdir
	} else if !strings.HasPrefix(dest, "/") {
		dest = path.Unix.Join(ctx.workdir, dest)
	}

	var baseDef common.BuildDefinition
	var filename = a.Src
	if strings.HasPrefix(a.Src, "http://") || strings.HasPrefix(a.Src, "https://") {
		baseDef = builder.Factory.NewFetchHttpBuildDefinition(a.Src, 0, nil)
		// basename for kind detection
		u, err := url.Parse(a.Src)
		if err == nil {
			filename = u.Path
		}
	} else {
		if !ctx.localConfig {
			return fmt.Errorf("local archive references are not allowed in remote configs: %s", a.Src)
		}
		if path.Native.IsAbs(a.Src) {
			return fmt.Errorf("absolute paths are not allowed in local config: %s", a.Src)
		}
		abs := path.Native.Clean(path.Native.Join(ctx.basePath, a.Src))
		if !strings.HasPrefix(abs+string(os.PathSeparator), ctx.basePath+string(os.PathSeparator)) && abs != ctx.basePath {
			return fmt.Errorf("path escapes base directory: %s", a.Src)
		}
		h, err := common.Sha256HashFromFile(abs)
		if err != nil {
			return err
		}
		baseDef = builder.Factory.NewConstantHashDefinition(h, func() (io.ReadCloser, error) { return os.Open(abs) })
		filename = abs
	}

	if kind, ok := builder.ReadArchiveSupportsExtracting(filename, false); ok {
		read := builder.Factory.NewReadArchiveBuildDefinition(baseDef, kind, 0)
		ctx.directives = append(ctx.directives, common.DirectiveArchive{Definition: read, Target: dest})
		return nil
	}
	if strings.HasSuffix(strings.ToLower(filename), ".archive") {
		ctx.directives = append(ctx.directives, common.DirectiveArchive{Definition: baseDef, Target: dest})
		return nil
	}
	return fmt.Errorf("no extractor for %s", a.Src)
}

type AppendV1Directive struct {
	Path string `yaml:"path"`
}

func parseV1VolumeToken(token string) (name string, sizeMB uint64, guestPath string, persist bool, err error) {
	parts := strings.Split(token, ",")
	if len(parts) < 3 {
		return "", 0, "", false, fmt.Errorf("invalid volume %s", token)
	}
	name = parts[0]
	sz := strings.ToLower(parts[1])
	mult := uint64(1)
	if strings.HasSuffix(sz, "g") {
		mult = 1024
		sz = strings.TrimSuffix(sz, "g")
	} else if strings.HasSuffix(sz, "m") {
		mult = 1
		sz = strings.TrimSuffix(sz, "m")
	} else if strings.HasSuffix(sz, "t") {
		mult = 1024 * 1024
		sz = strings.TrimSuffix(sz, "t")
	}
	v, perr := strconv.ParseUint(sz, 10, 64)
	if perr != nil {
		return "", 0, "", false, perr
	}
	sizeMB = v * mult
	guestPath = parts[2]
	if len(parts) >= 4 {
		persist = parts[3] == "persist"
	}
	return
}

// Apply implements directive.
func (a *AppendV1Directive) Apply(ctx *context) error {
	if a == nil || strings.TrimSpace(a.Path) == "" {
		return fmt.Errorf("v1_config path is required")
	}
	f, err := os.Open(a.Path)
	if err != nil {
		return err
	}
	defer f.Close()

	var cfg loginv1.Config
	dec := yaml.NewDecoder(f)
	if err := dec.Decode(&cfg); err != nil {
		return err
	}

	// Base/root arch mapping: prefer current ctx arch, ignore root override for now.
	// OCI image or plan
	if cfg.OciImage != "" {
		reg, img, tag, err := builder.ParseOciImage(cfg.OciImage)
		if err != nil {
			return err
		}
		ociArch, err := builder.ToOciArchitecture(ctx.arch)
		if err != nil {
			return err
		}
		ctx.directives = append(ctx.directives, builder.Factory.NewFetchOCIImageDefinition(reg, img, tag, ociArch))
	} else {
		// plan
		var pkgs []common.PackageQuery
		for _, p := range cfg.Packages {
			q, err := common.ParsePackageQuery(p)
			if err != nil {
				return err
			}
			pkgs = append(pkgs, q)
		}
		var tags common.TagList
		tags = append(tags, "level3", "defaults")
		if cfg.NoScripts || cfg.WriteRoot != "" {
			tags = append(tags, "noScripts")
		}
		plan, err := builder.Factory.NewPlanDefinition(cfg.Builder, ctx.arch, pkgs, tags)
		if err != nil {
			return err
		}
		ctx.directives = append(ctx.directives, plan)
	}

	// Environment
	if len(cfg.Environment) > 0 {
		var vars []string
		vars = append(vars, cfg.Environment...)
		ctx.directives = append(ctx.directives, common.DirectiveEnvironment{Variables: vars})
	}

	// Files
	for _, spec := range cfg.Files {
		src := spec
		dest := ""
		if i := strings.IndexAny(src, ":,"); i != -1 {
			dest = src[i+1:]
			src = src[:i]
		}
		// check if src is URL
		if u, err := url.Parse(src); err == nil && (u.Scheme == "http" || u.Scheme == "https") {
			fd := &FileDirective{URL: src, Name: dest}
			err := fd.Apply(ctx)
			if err != nil {
				return err
			}
		} else if path.Native.IsAbs(src) || strings.HasPrefix(src, ".") || strings.HasPrefix(src, "..") {
			// Resolve dest like file directive
			fd := &FileDirective{LocalPath: src, Name: dest}
			if err := fd.Apply(ctx); err != nil {
				return err
			}
		} else {
			return fmt.Errorf("invalid file source: %s", src)
		}
	}

	// Archives
	for _, spec := range cfg.Archives {
		src := spec
		dest := ""
		if i := strings.IndexAny(src, ":,"); i != -1 {
			dest = src[i+1:]
			src = src[:i]
		}
		ad := &ArchiveDirective{Src: src, Dest: dest}
		if err := ad.Apply(ctx); err != nil {
			return err
		}
	}

	// Volumes
	for _, v := range cfg.Volumes {
		name, sizeMB, guest, persist, err := parseV1VolumeToken(v)
		if err != nil {
			return err
		}
		ctx.directives = append(ctx.directives, common.DirectiveAddVolume{VolumeName: name, GuestPath: guest, MinimumSizeMB: sizeMB, Persist: persist})
	}

	// Ports
	forwarded := map[int]struct{}{}
	for _, p := range cfg.ForwardPorts {
		portStr := p
		if strings.Contains(portStr, ":") {
			_, portStr, _ = strings.Cut(portStr, ":")
		}
		pn, err := strconv.Atoi(portStr)
		if err != nil {
			return err
		}
		if _, ok := forwarded[pn]; ok {
			continue
		}
		forwarded[pn] = struct{}{}
		ctx.directives = append(ctx.directives, common.DirectiveExportPort{Name: "forward", ListenAddress: "localhost", Port: pn})
	}

	// Services
	for _, sc := range cfg.ServiceCommands {
		ctx.directives = append(ctx.directives, common.DirectiveStartServiceCommand{Command: sc})
	}

	// Layers then commands
	for _, layer := range cfg.Layers {
		if err := RunDirective([]string{layer}).Apply(ctx); err != nil {
			return err
		}
	}
	for _, cmd := range cfg.Commands {
		if err := RunDirective([]string{cmd}).Apply(ctx); err != nil {
			return err
		}
	}

	// Interaction
	if cfg.Init != "" {
		ctx.interaction = "init," + cfg.Init
	} else if cfg.WebSSH != "" {
		ctx.interaction = "webssh," + cfg.WebSSH
	}

	// Min spec / autoscale / storage
	if cfg.AutoScale {
		ctx.autoScale = true
	}
	if cfg.StorageSize > 0 {
		if ctx.storageSizeM == 0 || cfg.StorageSize > ctx.storageSizeM {
			ctx.storageSizeM = cfg.StorageSize
		}
	}

	return nil
}

type VolumeDirective struct {
	Name       string       `yaml:"name,omitempty"`
	MountPath  string       `yaml:"mount_path,omitempty"`
	Size       ByteQuantity `yaml:"size,omitempty"`
	Persistent bool         `yaml:"persistent,omitempty"`
}

// Apply implements directive.
func (v *VolumeDirective) Apply(ctx *context) error {
	if v == nil {
		return fmt.Errorf("volume directive is nil")
	}
	if v.Name == "" {
		return fmt.Errorf("volume name cannot be empty")
	}
	if v.MountPath == "" {
		return fmt.Errorf("volume mount_path cannot be empty")
	}
	bytes, err := v.Size.ToBytes()
	if err != nil {
		return err
	}
	if bytes <= 0 {
		return fmt.Errorf("volume size must be > 0")
	}
	sizeMB := uint64(bytes / (1024 * 1024))
	ctx.directives = append(ctx.directives, common.DirectiveAddVolume{
		VolumeName:    v.Name,
		GuestPath:     v.MountPath,
		MinimumSizeMB: sizeMB,
		Persist:       v.Persistent,
	})
	return nil
}

type MinSpecDirective struct {
	AutoScale bool         `yaml:"auto_scale,omitempty"`
	DiskSize  ByteQuantity `yaml:"disk_size,omitempty"`
}

// Apply implements directive.
func (m *MinSpecDirective) Apply(ctx *context) error {
	if m == nil {
		return fmt.Errorf("min_spec directive is nil")
	}
	ctx.autoScale = m.AutoScale
	if m.DiskSize != "" {
		bytes, err := m.DiskSize.ToBytes()
		if err != nil {
			return err
		}
		if bytes < 0 {
			return fmt.Errorf("disk_size must be >= 0")
		}
		ctx.storageSizeM = int(bytes / (1024 * 1024))
	}
	return nil
}

var (
	_ directive = (*PlanDirective)(nil)
	_ directive = (FromDirective)("")
	_ directive = (RunDirective)(nil)
	_ directive = (EnvironmentDirective)(nil)
	_ directive = (ExposeDirective)(nil)
	_ directive = (*VolumeDirective)(nil)
	_ directive = (*MinSpecDirective)(nil)
	_ directive = (WorkdirDirective)("")
	_ directive = (*FileDirective)(nil)
	_ directive = (*ArchiveDirective)(nil)
)

type LoginDirective struct {
	// Exactly one of the following should be set.
	Plan        *PlanDirective        `yaml:"plan,omitempty"`
	From        *FromDirective        `yaml:"from,omitempty"`
	Run         *RunDirective         `yaml:"run,omitempty"`
	Environment *EnvironmentDirective `yaml:"environment,omitempty"`
	Expose      *ExposeDirective      `yaml:"expose,omitempty"`
	Volume      *VolumeDirective      `yaml:"volume,omitempty"`
	MinSpec     *MinSpecDirective     `yaml:"min_spec,omitempty"`
	Workdir     *WorkdirDirective     `yaml:"workdir,omitempty"`
	File        *FileDirective        `yaml:"file,omitempty"`
	Archive     *ArchiveDirective     `yaml:"archive,omitempty"`
	Service     *ServiceDirective     `yaml:"service,omitempty"`
	V1Config    *AppendV1Directive    `yaml:"v1_config,omitempty"`
}

func (d *LoginDirective) AsDirective() (directive, error) {
	if d.Plan != nil {
		return d.Plan, nil
	} else if d.From != nil {
		return *d.From, nil
	} else if d.Run != nil {
		return *d.Run, nil
	} else if d.Environment != nil {
		return *d.Environment, nil
	} else if d.Expose != nil {
		return *d.Expose, nil
	} else if d.Volume != nil {
		return d.Volume, nil
	} else if d.MinSpec != nil {
		return d.MinSpec, nil
	} else if d.Workdir != nil {
		return *d.Workdir, nil
	} else if d.File != nil {
		return d.File, nil
	} else if d.Archive != nil {
		return d.Archive, nil
	} else if d.Service != nil {
		return *d.Service, nil
	} else if d.V1Config != nil {
		return d.V1Config, nil
	} else {
		return nil, fmt.Errorf("empty directive")
	}
}

type Config struct {
	Version int `yaml:"version"`

	Architecture string `yaml:"architecture,omitempty"`

	Directives []LoginDirective `yaml:"directives"`

	// runtime-only (not from YAML)
	basePath          string
	localConfig       bool
	writeTemplatePath bool
	writeTemplateHash bool
}

func (c *Config) SetWriteTemplatePath(enable bool) { c.writeTemplatePath = enable }
func (c *Config) SetWriteTemplateHash(enable bool) { c.writeTemplateHash = enable }

func (c *Config) Run(db common.PackageDatabase) error {
	if c.Version > CURRENT_CONFIG_VERSION {
		return fmt.Errorf("attempt to run config version %d on TinyRange version %d", c.Version, CURRENT_CONFIG_VERSION)
	}

	// Resolve architecture once for this run.
	arch, err := cfg.ArchitectureFromString(c.Architecture)
	if err != nil {
		return err
	}
	wd := "/"
	ctx := &context{arch: arch, workdir: wd, interaction: "ssh", db: db, basePath: c.basePath, localConfig: c.localConfig}

	for _, dir := range c.Directives {
		directive, err := dir.AsDirective()
		if err != nil {
			return err
		}

		if err := directive.Apply(ctx); err != nil {
			return err
		}
	}

	// If nothing schedules a command, default to interactive.
	hasRun := false
	for _, d := range ctx.directives {
		if _, ok := d.(common.DirectiveRunCommand); ok {
			hasRun = true
			break
		}
	}
	if !hasRun {
		ctx.directives = append(ctx.directives, common.DirectiveRunCommand{Command: "interactive"})
	}

	// Build and run a VM with the collected directives in the given order.
	vmDef := builder.Factory.NewBuildVmDefinition(
		ctx.directives,
		nil, nil, // kernel/initramfs defaults
		"",
		0, 0, ctx.autoScale,
		arch, arch,
		ctx.storageSizeM,
		ctx.interaction,
		"",
		false,
	)

	if c.writeTemplatePath || c.writeTemplateHash {
		vmDef.SetBuildTemplateMode()
	}

	if _, err := db.Builder().Build(vmDef, common.BuildOptions{AlwaysRebuild: true}); err != nil {
		var built common.ErrTemplateBuilt
		if (c.writeTemplatePath || c.writeTemplateHash) && errors.As(err, &built) {
			if c.writeTemplatePath {
				fmt.Printf("%s\n", built.Filename)
			}
			if c.writeTemplateHash {
				fmt.Printf("%s\n", string(built.Hash))
			}
			return nil
		}
		return err
	}

	return nil
}

// SetBasePath sets the base directory for resolving local paths in a sandboxed manner.
func (c *Config) SetBasePath(p string) { c.basePath = p }

// SetLocalConfig marks this config as local, enabling sandboxing checks on local file references.
func (c *Config) SetLocalConfig() { c.localConfig = true }

func Load(r io.ReaderAt) (*Config, error) {
	var config Config
	dec := yaml.NewDecoder(io.NewSectionReader(r, 0, 1<<20))
	if err := dec.Decode(&config); err != nil {
		return nil, err
	}
	return &config, nil
}

package v2

import (
	"fmt"
	"io"
	"strconv"

	"gopkg.in/yaml.v3"

	"github.com/tinyrange/tinyrange/pkg/common"
)

const CURRENT_CONFIG_VERSION = 2

type ByteQuantity string

func (bq ByteQuantity) ToBytes() (int64, error) {
	var multiplier int64 = 1024 * 1024 // default to megabytes

	s := string(bq)
	n := len(s)

	if n == 0 {
		return 0, fmt.Errorf("invalid byte quantity: empty string")
	}

	lastChar := s[n-1]
	if lastChar == 'K' || lastChar == 'M' || lastChar == 'G' || lastChar == 'T' {
		switch lastChar {
		case 'b':
			multiplier = 1
		case 'K':
			multiplier = 1024
		case 'M':
			multiplier = 1024 * 1024
		case 'G':
			multiplier = 1024 * 1024 * 1024
		case 'T':
			multiplier = 1024 * 1024 * 1024 * 1024
		}
		s = s[:n-1]
	}

	value, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid byte quantity: %w", err)
	}

	return value * multiplier, nil
}

type context struct {
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
	panic("unimplemented")
}

type FromDirective string

// Apply implements directive.
func (f FromDirective) Apply(ctx *context) error {
	panic("unimplemented")
}

type RunDirective []string

// Apply implements directive.
func (r RunDirective) Apply(ctx *context) error {
	panic("unimplemented")
}

type EnvironmentDirective map[string]string

// Apply implements directive.
func (e EnvironmentDirective) Apply(ctx *context) error {
	panic("unimplemented")
}

type ExposeDirective []int

// Apply implements directive.
func (e ExposeDirective) Apply(ctx *context) error {
	panic("unimplemented")
}

type VolumeDirective struct {
	Name       string       `yaml:"name,omitempty"`
	MountPath  string       `yaml:"mount_path,omitempty"`
	Size       ByteQuantity `yaml:"size,omitempty"`
	Persistent bool         `yaml:"persistent,omitempty"`
}

// Apply implements directive.
func (v *VolumeDirective) Apply(ctx *context) error {
	panic("unimplemented")
}

type MinSpecDirective struct {
	AutoScale bool         `yaml:"auto_scale,omitempty"`
	DiskSize  ByteQuantity `yaml:"disk_size,omitempty"`
}

// Apply implements directive.
func (m *MinSpecDirective) Apply(ctx *context) error {
	panic("unimplemented")
}

var (
	_ directive = (*PlanDirective)(nil)
	_ directive = (FromDirective)("")
	_ directive = (RunDirective)(nil)
	_ directive = (EnvironmentDirective)(nil)
	_ directive = (ExposeDirective)(nil)
	_ directive = (*VolumeDirective)(nil)
	_ directive = (*MinSpecDirective)(nil)
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
	} else {
		return nil, fmt.Errorf("empty directive")
	}
}

type Config struct {
	Version int `yaml:"version"`

	Architecture string `yaml:"architecture,omitempty"`

	Directives []LoginDirective `yaml:"directives"`
}

func (c *Config) Run(db common.PackageDatabase) error {
	ctx := &context{}

	for _, dir := range c.Directives {
		directive, err := dir.AsDirective()
		if err != nil {
			return err
		}

		if err := directive.Apply(ctx); err != nil {
			return err
		}
	}

	return fmt.Errorf("not implemented yet")
}

func Load(r io.ReaderAt) (*Config, error) {
	var config Config
	dec := yaml.NewDecoder(io.NewSectionReader(r, 0, 1<<20))
	if err := dec.Decode(&config); err != nil {
		return nil, err
	}
	return &config, nil
}

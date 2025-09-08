package v2

import (
	"fmt"
	"io"

	"gopkg.in/yaml.v3"

	"github.com/tinyrange/tinyrange/pkg/common"
)

const CURRENT_CONFIG_VERSION = 2

type Config struct {
	Version int `yaml:"version"`
}

func (c *Config) Run(db common.PackageDatabase) error {
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

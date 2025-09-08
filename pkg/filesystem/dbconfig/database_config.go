package dbconfig

import (
	"fmt"
	"net/url"

	"github.com/tinyrange/tinyrange/pkg/path"
)

type RelativeHostBuildDirectory struct {
	RelativePath string `json:"relative_path" yaml:"relative_path"`
}

func (cfg RelativeHostBuildDirectory) Validate() error {
	if cfg.RelativePath == "" {
		return fmt.Errorf("relative_path is required")
	}

	// the relative path mustn't be absolute
	if path.Native.IsAbs(cfg.RelativePath) {
		return fmt.Errorf("relative_path must be relative: %s", cfg.RelativePath)
	}

	return nil
}

type AbsoluteHostBuildDirectory struct {
	AbsolutePath string `json:"absolute_path" yaml:"absolute_path"`
}

func (cfg AbsoluteHostBuildDirectory) Validate() error {
	if cfg.AbsolutePath == "" {
		return fmt.Errorf("absolute_path is required")
	}

	// the absolute path must be absolute
	if !path.Native.IsAbs(cfg.AbsolutePath) {
		return fmt.Errorf("absolute_path must be absolute: %s", cfg.AbsolutePath)
	}

	return nil
}

type Archive2BuildArtifact struct {
	// Assumes that the archive exists in a already mounted filesystem.
	Hash string `json:"hash" yaml:"hash"`
	Path string `json:"path" yaml:"path"`
}

func (cfg Archive2BuildArtifact) Validate() error {
	if cfg.Hash == "" {
		return fmt.Errorf("hash is required")
	}

	return nil
}

type RemoteBuildDirectory struct {
	BaseURL string `json:"base_url" yaml:"base_url"`
}

func (cfg RemoteBuildDirectory) Validate() error {
	url, err := url.Parse(cfg.BaseURL)
	if err != nil {
		return fmt.Errorf("invalid base_url: %s", cfg.BaseURL)
	}

	if url.Scheme != "http" && url.Scheme != "https" {
		return fmt.Errorf("base_url must be http or https: %s", cfg.BaseURL)
	}

	if url.Host == "" {
		return fmt.Errorf("base_url must have a host: %s", cfg.BaseURL)
	}

	return nil
}

type DefaultBuildDirectory struct {
}

func (cfg DefaultBuildDirectory) Validate() error {
	// Default build directory is always valid.
	return nil
}

type BuildDatabaseConfig struct {
	DefaultBuildDirectory      *DefaultBuildDirectory      `json:"default_build_directory,omitempty" yaml:"default_build_directory,omitempty"`
	RelativeHostBuildDirectory *RelativeHostBuildDirectory `json:"relative_host_build_directory,omitempty" yaml:"relative_host_build_directory,omitempty"`
	AbsoluteHostBuildDirectory *AbsoluteHostBuildDirectory `json:"absolute_host_build_directory,omitempty" yaml:"absolute_host_build_directory,omitempty"`
	Archive2BuildArtifact      *Archive2BuildArtifact      `json:"archive2_build_artifact,omitempty" yaml:"archive2_build_artifact,omitempty"`
	RemoteBuildDirectory       *RemoteBuildDirectory       `json:"remote_build_directory,omitempty" yaml:"remote_build_directory,omitempty"`
}

func (cfg BuildDatabaseConfig) Validate() error {
	if cfg.RelativeHostBuildDirectory != nil {
		return cfg.RelativeHostBuildDirectory.Validate()
	} else if cfg.AbsoluteHostBuildDirectory != nil {
		return cfg.AbsoluteHostBuildDirectory.Validate()
	} else if cfg.Archive2BuildArtifact != nil {
		return cfg.Archive2BuildArtifact.Validate()
	} else if cfg.RemoteBuildDirectory != nil {
		return cfg.RemoteBuildDirectory.Validate()
	} else if cfg.DefaultBuildDirectory != nil {
		return cfg.DefaultBuildDirectory.Validate()
	} else {
		return fmt.Errorf("invalid build database config: %v", cfg)
	}
}

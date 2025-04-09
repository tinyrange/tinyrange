package filesystem

import (
	"fmt"

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
}

func (cfg Archive2BuildArtifact) Validate() error {
	if cfg.Hash == "" {
		return fmt.Errorf("hash is required")
	}

	return nil
}

type BuildDatabaseConfig struct {
	RelativeHostBuildDirectory *RelativeHostBuildDirectory `json:"relative_host_build_directory" yaml:"relative_host_build_directory"`
	AbsoluteHostBuildDirectory *AbsoluteHostBuildDirectory `json:"absolute_host_build_directory" yaml:"absolute_host_build_directory"`
	Archive2BuildArtifact      *Archive2BuildArtifact      `json:"archive2_build_artifact" yaml:"archive2_build_artifact"`
}

func (cfg BuildDatabaseConfig) Validate() error {
	if cfg.RelativeHostBuildDirectory != nil {
		return cfg.RelativeHostBuildDirectory.Validate()
	} else if cfg.AbsoluteHostBuildDirectory != nil {
		return cfg.AbsoluteHostBuildDirectory.Validate()
	} else if cfg.Archive2BuildArtifact != nil {
		return cfg.Archive2BuildArtifact.Validate()
	} else {
		return fmt.Errorf("invalid build database config: %v", cfg)
	}
}

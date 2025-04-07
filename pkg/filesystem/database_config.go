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

type BuildDatabaseConfig struct {
	RelativeHostBuildDirectory *RelativeHostBuildDirectory `json:"relative_host_build_directory" yaml:"relative_host_build_directory"`
	AbsoluteHostBuildDirectory *AbsoluteHostBuildDirectory `json:"absolute_host_build_directory" yaml:"absolute_host_build_directory"`
}

func (cfg BuildDatabaseConfig) Validate() error {
	if cfg.RelativeHostBuildDirectory != nil {
		return cfg.RelativeHostBuildDirectory.Validate()
	} else if cfg.AbsoluteHostBuildDirectory != nil {
		return cfg.AbsoluteHostBuildDirectory.Validate()
	} else {
		return fmt.Errorf("invalid build database config: %v", cfg)
	}
}

func getRealPathForDirectory(dir Directory) (string, error) {
	switch dir := dir.(type) {
	case *localDirectory:
		return dir.filename, nil
	case *localMutableDirectory:
		return dir.filename, nil
	default:
		return "", fmt.Errorf("unknown directory type: %T", dir)
	}
}

func getConfigFromHostFilename(topSyntheticPath string, targetPath string, isPrimary bool) (BuildDatabaseConfig, error) {
	if isPrimary {
		relativePath, err := path.Native.Rel(topSyntheticPath, targetPath)
		if err != nil {
			return BuildDatabaseConfig{}, fmt.Errorf("failed to get relative path: %w", err)
		}

		return BuildDatabaseConfig{
			RelativeHostBuildDirectory: &RelativeHostBuildDirectory{
				RelativePath: relativePath,
			},
		}, nil
	} else {
		absPath, err := path.Native.Abs(targetPath)
		if err != nil {
			return BuildDatabaseConfig{}, fmt.Errorf("failed to get absolute path: %w", err)
		}

		return BuildDatabaseConfig{
			AbsoluteHostBuildDirectory: &AbsoluteHostBuildDirectory{
				AbsolutePath: absPath,
			},
		}, nil
	}
}

func GetDatabaseConfigForDirectory(top Directory, dir Directory) (BuildDatabaseConfig, error) {
	topRealPath, err := getRealPathForDirectory(top)
	if err != nil {
		return BuildDatabaseConfig{}, fmt.Errorf("failed to get real path for top directory: %w", err)
	}

	topSyntheticPath := path.Native.Join(topRealPath, "synthetic/synthetic")

	switch dir := dir.(type) {
	case *localDirectory:
		return getConfigFromHostFilename(topSyntheticPath, dir.filename, top == dir)
	case *localMutableDirectory:
		return getConfigFromHostFilename(topSyntheticPath, dir.filename, top == dir)
	default:
		return BuildDatabaseConfig{}, fmt.Errorf("unknown directory type: %T", dir)
	}
}

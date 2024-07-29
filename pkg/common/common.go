package common

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	starlarkjson "go.starlark.net/lib/json"
	"go.starlark.net/starlark"
)

const TINYRANGE_NAME = "tinyrange"

var StarlarkJsonEncode = starlarkjson.Module.Members["encode"].(*starlark.Builtin).CallInternal
var StarlarkJsonDecode = starlarkjson.Module.Members["decode"].(*starlark.Builtin).CallInternal

func ToStringList(it starlark.Iterable) ([]string, error) {
	iter := it.Iterate()
	defer iter.Done()

	var ret []string

	var val starlark.Value
	for iter.Next(&val) {
		str, ok := starlark.AsString(val)
		if !ok {
			return nil, fmt.Errorf("could not convert %s to string", val.Type())
		}

		ret = append(ret, str)
	}

	return ret, nil
}

// From: https://stackoverflow.com/questions/12518876/how-to-check-if-a-file-exists-in-go
func Exists(name string) (bool, error) {
	_, err := os.Stat(name)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

func Ensure(path string, mode os.FileMode) error {
	err := os.MkdirAll(path, mode)
	if err != nil {
		return fmt.Errorf("failed to create directory: %v", err)
	}

	return nil
}

type CPUArchitecture string

const (
	ArchX8664 CPUArchitecture = "x86_64"
)

func (arch CPUArchitecture) IsNative() bool {
	switch runtime.GOARCH {
	case "amd64":
		return arch == ArchX8664
	default:
		panic("unknown architecture: " + arch)
	}
}

func getExeDirectory() (string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return "", err
	}

	return filepath.Dir(exePath), nil
}

func GetAdjacentExecutable(name string) (string, error) {
	exeDir, err := getExeDirectory()
	if err != nil {
		return "", err
	}

	localPath := filepath.Join(exeDir, name)

	if ok, _ := Exists(localPath); ok {
		return localPath, nil
	}

	return exec.LookPath(name)
}

func GetTinyRangeExecutable() (string, error) {
	// TODO(joshua): Don't assume that the TinyRange executable is always called tinyrange.
	// Maybe allow setting it with a environment variable?
	return GetAdjacentExecutable(TINYRANGE_NAME)
}

func GetDefaultBuildDir() string {
	// Look for the tinyrange.portable file first.
	exeDir, err := getExeDirectory()
	if err != nil {
		slog.Warn("Could not get executable directory. Builds will default to the current directory under build.", "err", err)
		return "build"
	}

	// If that exists then put the build dir next to our current executables.
	if ok, _ := Exists(filepath.Join(exeDir, "tinyrange.portable")); ok {
		return filepath.Join(exeDir, "build")
	}

	// Otherwise find the user cache directory...
	cache, err := os.UserCacheDir()
	if err != nil {
		slog.Warn("Could not get executable directory. Builds will default to the current directory under build.", "err", err)
		return "build"
	}

	// and create a build directory under that.
	return filepath.Join(cache, "tinyrange", "build")
}

const REPO_PATH = ""

package common

import (
	"embed"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/anmitsu/go-shlex"
	starlarkjson "go.starlark.net/lib/json"
	"go.starlark.net/starlark"
)

var StarlarkJsonEncode = starlarkjson.Module.Members["encode"].(*starlark.Builtin).CallInternal
var StarlarkJsonDecode = starlarkjson.Module.Members["decode"].(*starlark.Builtin).CallInternal

var SOURCE_FS embed.FS

func SetSourceFS(fs embed.FS) {
	SOURCE_FS = fs
}

var verboseEnabled = false

func EnableVerbose() error {
	verboseEnabled = true

	slog.SetLogLoggerLevel(slog.LevelDebug)

	if err := os.Setenv("TINYRANGE_VERBOSE", "on"); err != nil {
		return err
	}

	return nil
}

func IsVerbose() bool {
	return verboseEnabled
}

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

func ExecCommand(args []string, environment map[string]string) error {
	if ok, _ := Exists(args[0]); !ok {
		return fmt.Errorf("path %s does not exist", args[0])
	}

	cmd := exec.Command(args[0], args[1:]...)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = cmd.Environ()

	for k, v := range environment {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	err := cmd.Run()
	if exit, ok := err.(*exec.ExitError); ok {
		if exit.ExitCode() == 255 {
			slog.Warn("command returned exit 255", "args", args)
			return nil
		}
	} else if err != nil {
		return err
	}

	return nil
}

func RunCommand(script string) error {
	if strings.HasPrefix(script, "/init") {
		tokens, err := shlex.Split(script, true)
		if err != nil {
			return err
		}

		return ExecCommand(tokens, nil)
	} else if script == "interactive" {
		return ExecCommand([]string{"/bin/login", "-pf", "root"}, nil)
	} else {
		return ExecCommand([]string{"/bin/sh", "-lc", script}, nil)
	}
}

const REPO_PATH = ""

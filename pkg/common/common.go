package common

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/anmitsu/go-shlex"
	"github.com/tinyrange/tinyrange/pkg/feature"
	"github.com/tinyrange/tinyrange/pkg/path"
	starlarkjson "go.starlark.net/lib/json"
	"go.starlark.net/starlark"
)

var StarlarkJsonEncode = starlarkjson.Module.Members["encode"].(*starlark.Builtin).CallInternal
var StarlarkJsonDecode = starlarkjson.Module.Members["decode"].(*starlark.Builtin).CallInternal

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

	return path.Native.Dir(exePath), nil
}

func GetAdjacentExecutable(names ...string) (string, error) {
	exeDir, err := getExeDirectory()
	if err != nil {
		return "", err
	}

	for _, name := range names {
		localPath := path.Native.Join(exeDir, name)

		if runtime.GOOS == "windows" {
			localPath += ".exe"
		}

		if ok, _ := Exists(localPath); ok {
			return localPath, nil
		}

		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}

	return "", fmt.Errorf("could not find any of the executables: %v", names)
}

func GetAdjacentFile(names ...string) (string, error) {
	exeDir, err := getExeDirectory()
	if err != nil {
		return "", err
	}

	for _, name := range names {
		localPath := path.Native.Join(exeDir, name)

		if ok, _ := Exists(localPath); ok {
			return localPath, nil
		}

		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}

	return "", fmt.Errorf("could not find any of the files: %v", names)
}

func IsPortable() bool {
	exeDir, err := getExeDirectory()
	if err != nil {
		return false
	}

	if ok, _ := Exists(path.Native.Join(exeDir, "tinyrange.portable")); ok {
		return true
	}

	return false
}

func GetDefaultBuildDir() string {
	// Look for the tinyrange.portable file first.
	exeDir, err := getExeDirectory()
	if err != nil {
		slog.Warn("Could not get executable directory. Builds will default to the current directory under build.", "err", err)
		return "build"
	}

	// If that exists then put the build dir next to our current executables.
	if ok, _ := Exists(path.Native.Join(exeDir, "tinyrange.portable")); ok {
		return path.Native.Join(exeDir, "build")
	}

	// Otherwise find the user cache directory...
	cache, err := os.UserCacheDir()
	if err != nil {
		slog.Warn("Could not get executable directory. Builds will default to the current directory under build.", "err", err)
		return "build"
	}

	// and create a build directory under that.
	return path.Native.Join(cache, "tinyrange", "build")
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

func ExecService(args []string, environment map[string]string) (*exec.Cmd, error) {
	if ok, _ := Exists(args[0]); !ok {
		return nil, fmt.Errorf("path %s does not exist", args[0])
	}

	cmd := exec.Command(args[0], args[1:]...)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = cmd.Environ()

	for k, v := range environment {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	err := cmd.Start()
	if err != nil {
		return nil, err
	}

	return cmd, nil
}

var DefaultInteractiveCommand = []string{"/bin/sh"}

func SetDefaultInteractive(args []string) {
	DefaultInteractiveCommand = args
}

func RunCommand(script string) error {
	if strings.HasPrefix(script, "/init") {
		tokens, err := shlex.Split(script, true)
		if err != nil {
			return err
		}

		return ExecCommand(tokens, nil)
	} else if script == "interactive" {
		return ExecCommand(DefaultInteractiveCommand, nil)
	} else {
		return ExecCommand([]string{"/bin/sh", "-lc", script}, nil)
	}
}

func RunService(script string) (*exec.Cmd, error) {
	if strings.HasPrefix(script, "/init") {
		tokens, err := shlex.Split(script, true)
		if err != nil {
			return nil, err
		}

		return ExecService(tokens, nil)
	} else {
		return ExecService([]string{"/bin/sh", "-lc", script}, nil)
	}
}

func SetExperimental(flags []string) error {
	if err := os.Setenv("TINYRANGE_EXPERIMENTAL", strings.Join(flags, ",")); err != nil {
		return err
	}

	feature.SetFeaturesFromExperimentalFlags(flags)

	return nil
}

const REPO_PATH = ""

func CopyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("could not open source file: %v", err)
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("could not create destination file: %v", err)
	}
	defer out.Close()

	if _, err = io.Copy(out, in); err != nil {
		return fmt.Errorf("could not copy file: %v", err)
	}

	return nil
}

func Sha256HashFromReader(r io.Reader) (string, error) {
	h := sha256.New()

	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

func Sha256HashFromFile(filename string) (string, error) {
	f, err := os.Open(filename)
	if err != nil {
		return "", err
	}
	defer f.Close()

	return Sha256HashFromReader(f)
}

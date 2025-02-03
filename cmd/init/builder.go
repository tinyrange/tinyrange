//go:build linux

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/config"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/path"
	shelltranslater "github.com/tinyrange/tinyrange/pkg/shellTranslater"
	"go.starlark.net/starlark"
	"golang.org/x/sys/unix"
)

type Builder struct {
	translateShell bool

	totalTranslate     time.Duration
	totalRunTranslated time.Duration
	totalRunCommand    time.Duration
}

// OnBuiltin implements shelltranslater.Notifier.
func (b *Builder) OnBuiltin(name string, args []string) {
	if !common.IsVerbose() {
		return
	}

	fmt.Fprintf(os.Stderr, "||   builtin.%s(%+v)\n", name, args)
}

// PostRunShell implements shelltranslater.Notifier.
func (b *Builder) PostRunShell(args []string, exit int, took time.Duration) {
	if !common.IsVerbose() {
		return
	}

	fmt.Fprintf(os.Stderr, "||   < runShell(%+v) = %d [%s]\n", args, exit, took)
}

// PreRunShell implements shelltranslater.Notifier.
func (b *Builder) PreRunShell(args []string) {
	if !common.IsVerbose() {
		return
	}

	fmt.Fprintf(os.Stderr, "||   > runShell(%+v)\n", args)
}

func (b *Builder) uploadFile(address string, filename string) error {
	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	url := fmt.Sprintf("http://%s/upload_output", address)

	resp, err := http.Post(url, "application/binary", f)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}

func (b *Builder) translateAndRun(args []string, environment map[string]string) (bool, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return false, err
	}
	defer os.Chdir(cwd)

	transpileStart := time.Now()

	f, err := os.Open(args[0])
	if err != nil {
		return false, err
	}
	defer f.Close()

	sh := shelltranslater.NewTranspiler(true, true)

	translated, err := sh.TranslateFile(f, args[0])
	if err != nil {
		return false, err
	}

	transpileTime := time.Since(transpileStart)

	runStart := time.Now()

	rt := shelltranslater.NewRuntime(true, b)

	if common.IsVerbose() {
		fmt.Fprintf(os.Stderr, "|| > translated(%s)\n", args[0])
	}

	if err := rt.Run(args[0], translated, args, environment); err != nil {
		return true, err
	}

	runTime := time.Since(runStart)

	if common.IsVerbose() {
		fmt.Fprintf(os.Stderr, "|| < translated(%s) [transpile=%s, run=%s]\n", args[0], transpileTime, runTime)
		b.totalTranslate += transpileTime
		b.totalRunTranslated += runTime
	}

	return true, nil
}

func (b *Builder) execCommand(args []string, env map[string]string) error {
	if b.translateShell {
		fatal, err := b.translateAndRun(args, env)
		if err != nil {
			if fatal {
				return fmt.Errorf("failed to translate and run: %s", err)
			} else {
				if common.IsVerbose() {
					fmt.Fprintf(os.Stderr, "|| W translate(%s) = %s\n", args[0], err)
				}
			}
		} else {
			return nil
		}
	}

	start := time.Now()

	if err := common.ExecCommand(args, env); err != nil {
		return fmt.Errorf("failed to run command: %s", err)
	}

	if common.IsVerbose() {
		b.totalRunCommand += time.Since(start)
	}

	return nil
}

type BuilderScript struct {
	Kind        string            `json:"kind"`
	Triggers    []string          `json:"triggers"`
	Exec        string            `json:"exec"`
	Arguments   []string          `json:"args"`
	Environment map[string]string `json:"env"`
}

func (b *Builder) runScript(script BuilderScript) error {
	switch script.Kind {
	case "trigger_on":
		start := time.Now()

		args := []string{}

		for _, trigger := range script.Triggers {
			if ok, _ := common.Exists(trigger); !ok {
				continue
			}

			args = append(args, trigger)
		}

		if len(args) == 0 {
			return nil
		}

		if common.IsVerbose() {
			fmt.Fprintf(os.Stderr, "|| > trigger_on(%s, %s)\n", script.Exec, args)
		}

		if err := b.execCommand(
			append([]string{script.Exec}, args...),
			script.Environment,
		); err != nil {
			return err
		}

		if common.IsVerbose() {
			fmt.Fprintf(os.Stderr, "|| < trigger_on(%s, %s) [%s]\n", script.Exec, args, time.Since(start))
		}

		return nil
	case "execute":
		start := time.Now()

		if common.IsVerbose() {
			fmt.Fprintf(os.Stderr, "|| > execute(%s, %+v)\n", script.Exec, script.Arguments)
		}

		if err := b.execCommand(
			append([]string{script.Exec}, script.Arguments...),
			script.Environment,
		); err != nil {
			return err
		}

		if common.IsVerbose() {
			fmt.Fprintf(os.Stderr, "|| < execute(%s, %+v) [%s]\n", script.Exec, script.Arguments, time.Since(start))
		}

		return nil
	default:
		return fmt.Errorf("unknown kind: %s", script.Kind)
	}
}

func (b *Builder) RunScripts(filename string) error {
	f, err := os.Open(filename)
	if err != nil {
		return err
	}

	dec := json.NewDecoder(f)

	var scripts []BuilderScript

	if err := dec.Decode(&scripts); err != nil {
		return err
	}

	start := time.Now()

	if common.IsVerbose() {
		fmt.Fprintf(os.Stderr, "Started running %s at %s\nUsing Basic Environment:\n",
			filename, time.Now().Format(time.RFC1123))

		for _, val := range os.Environ() {
			fmt.Fprintf(os.Stderr, "- %s\n", val)
		}

		fmt.Fprintf(os.Stderr, "\n")
	}

	for _, script := range scripts {
		if err := b.runScript(script); err != nil {
			return err
		}

		if common.IsVerbose() {
			fmt.Fprintf(os.Stderr, "\n\n")
		}
	}

	if common.IsVerbose() {
		fmt.Fprintf(os.Stderr, "Finished running %s at %s [%s]\nTotal Translate Time: %s\nTotal Translated Runtime: %s\nTotal Regular Runtime: %s\n",
			filename, time.Now().Format(time.RFC1123), time.Since(start),
			b.totalTranslate, b.totalRunTranslated, b.totalRunCommand)
	}

	return nil
}

type changeTracker struct {
	// unix microseconds since epoch. 0 if this is a directory.
	ModTime  int64                     `json:"m"`
	Children map[string]*changeTracker `json:"c"`
}

func (builder *Builder) writeChangeTracker(outputFilename string) error {
	mountList, err := common.GetMounts()
	if err != nil {
		return err
	}

	mounts := make(map[string]common.MountInfo)

	for _, mount := range mountList {
		mounts[mount.Target] = mount
	}

	var enumerate func(filename string) (*changeTracker, error)

	enumerate = func(filename string) (*changeTracker, error) {
		mount, ok := mounts[filename]
		if ok {
			if mount.Kind != "rootfs" && mount.Kind != "ext4" {
				return nil, nil
			}
		}

		info, err := os.Lstat(filename)
		if err != nil {
			return nil, err
		}

		if info.IsDir() {
			ret := &changeTracker{
				ModTime:  0,
				Children: make(map[string]*changeTracker),
			}

			ents, err := os.ReadDir(filename)
			if err != nil {
				return nil, err
			}

			for _, ent := range ents {
				child, err := enumerate(path.Native.Join(filename, ent.Name()))
				if err != nil {
					return nil, err
				}

				if child != nil {
					ret.Children[ent.Name()] = child
				}
			}

			return ret, nil
		} else {
			return &changeTracker{
				ModTime: info.ModTime().UnixMicro(),
			}, nil
		}
	}

	top, err := enumerate("/")
	if err != nil {
		return err
	}

	f, err := os.Create(outputFilename)
	if err != nil {
		return err
	}
	defer f.Close()

	w := json.NewEncoder(f)

	if err := w.Encode(&top); err != nil {
		return err
	}

	return nil
}

func (builder *Builder) uploadChangedArchive(hostAddress string, changeTrackerFilename string) error {
	mountList, err := common.GetMounts()
	if err != nil {
		return err
	}

	mounts := make(map[string]common.MountInfo)

	for _, mount := range mountList {
		mounts[mount.Target] = mount
	}

	var tracker *changeTracker

	in, err := os.Open(changeTrackerFilename)
	if err != nil {
		return err
	}
	defer in.Close()

	dec := json.NewDecoder(in)

	if err := dec.Decode(&tracker); err != nil {
		return err
	}

	var enumerate func(tracker *changeTracker, filename string) error

	var changedFiles []string

	enumerate = func(tracker *changeTracker, filename string) error {
		mount, ok := mounts[filename]
		if ok {
			if mount.Kind != "rootfs" && mount.Kind != "ext4" {
				return nil
			}
		}

		if tracker == nil {
			// If there's no tracker then it's definitely changed.
			// This is either a new file or a new folder.
			changedFiles = append(changedFiles, filename)
			return nil
		}

		if tracker.ModTime != 0 {
			// assume this is a file.
			info, err := os.Lstat(filename)
			if err != nil {
				return err
			}

			if info.ModTime().UnixMicro() > tracker.ModTime {
				changedFiles = append(changedFiles, filename)
			}

			return nil
		}

		// Assume this is a directory.
		ents, err := os.ReadDir(filename)
		if err != nil {
			return err
		}

		for _, ent := range ents {
			child := tracker.Children[ent.Name()]

			if err := enumerate(child, path.Native.Join(filename, ent.Name())); err != nil {
				return err
			}
		}

		return nil
	}

	if err := enumerate(tracker, "/"); err != nil {
		return err
	}

	errors := make(chan error)
	done := make(chan bool)
	wg := sync.WaitGroup{}

	pipeOut, pipeIn := io.Pipe()

	wg.Add(1)
	go func() {
		defer close(errors)
		defer wg.Done()

		url := fmt.Sprintf("http://%s/upload_output", hostAddress)

		resp, err := http.Post(url, "application/binary", pipeOut)
		if err != nil {
			errors <- err
			return
		}
		defer resp.Body.Close()
	}()

	wg.Add(1)
	go func() {
		defer pipeIn.Close()
		defer wg.Done()

		ark := filesystem.NewArchiveWriter(pipeIn)

		var writeFile func(filename string) error

		writeFile = func(filename string) error {
			info, err := os.Lstat(filename)
			if err != nil {
				return err
			}

			sys := info.Sys().(*syscall.Stat_t)

			switch info.Mode().Type() {
			case 0: // regular file
				contents, err := os.Open(filename)
				if err != nil {
					return err
				}
				defer contents.Close()

				if err := ark.WriteEntry(&filesystem.CacheEntry{
					CTypeflag: filesystem.TypeRegular,
					CName:     filename,
					CSize:     info.Size(),
					CMode:     int64(info.Mode()),
					CUid:      int(sys.Uid),
					CGid:      int(sys.Gid),
					CModTime:  info.ModTime().UnixMicro(),
				}, contents); err != nil {
					return err
				}

				return nil
			case fs.ModeDir:
				if err := ark.WriteEntry(&filesystem.CacheEntry{
					CTypeflag: filesystem.TypeDirectory,
					CName:     filename,
					CSize:     0,
					CMode:     int64(info.Mode()),
					CUid:      int(sys.Uid),
					CGid:      int(sys.Gid),
					CModTime:  info.ModTime().UnixMicro(),
				}, nil); err != nil {
					return err
				}

				ents, err := os.ReadDir(filename)
				if err != nil {
					return err
				}

				for _, ent := range ents {
					child := path.Native.Join(filename, ent.Name())

					if err := writeFile(child); err != nil {
						return err
					}
				}

				return nil
			case fs.ModeSymlink:
				linkName, err := os.Readlink(filename)
				if err != nil {
					return err
				}

				if err := ark.WriteEntry(&filesystem.CacheEntry{
					CTypeflag: filesystem.TypeSymlink,
					CName:     filename,
					CLinkname: linkName,
					CSize:     0,
					CMode:     int64(info.Mode()),
					CUid:      int(sys.Uid),
					CGid:      int(sys.Gid),
					CModTime:  info.ModTime().UnixMicro(),
				}, nil); err != nil {
					return err
				}

				return nil
			default:
				return fmt.Errorf("unknown file type: %s", info.Mode())
			}
		}

		for _, file := range changedFiles {
			if file == changeTrackerFilename {
				continue
			}

			if err := writeFile(file); err != nil {
				errors <- err
				return
			}
		}
	}()

	go func() {
		wg.Wait()

		done <- true
	}()

	for {
		select {
		case err := <-errors:
			return err
		case <-done:
			return nil
		}
	}
}

func getString(dict *starlark.Dict, key string) (string, error) {
	val, found, err := dict.Get(starlark.String(key))
	if err != nil {
		return "", err
	}

	if !found {
		return "", nil
	}

	str, ok := val.(starlark.String)
	if !ok {
		return "", fmt.Errorf("key %s is not a string", key)
	}

	return string(str), nil
}

func (b *Builder) forkSSHServer() error {
	cmd, err := os.Executable()
	if err != nil {
		return err
	}

	command := exec.Command(cmd, "-ssh-configured")

	command.Stdout = os.Stdout
	command.Stderr = os.Stderr

	if err := command.Start(); err != nil {
		return err
	}

	return nil
}

func builderRunScripts(filename string, translateShell bool) error {
	builder := &Builder{translateShell: translateShell}

	return builder.RunScripts(filename)
}

// regex to match $ENV so long as it doesn't have $ before it.
var envRegex = regexp.MustCompile(`([^$])\$([A-Z_]+)`)

func builderRunWithConfig(cfg config.BuilderConfig) error {
	if len(cfg.DefaultInteractive) > 0 {
		common.SetDefaultInteractive(cfg.DefaultInteractive)
	}

	builder := &Builder{}

	// run init scripts first
	for _, script := range cfg.InitScripts {
		if err := runStarlarkFile(script); err != nil {
			return err
		}
	}

	// run starlark scripts
	for _, script := range cfg.StarlarkScripts {
		if err := runStarlarkScript("script.star", script); err != nil {
			return err
		}
	}

	var appendProfile strings.Builder

	for _, env := range cfg.Environment {
		k, v, _ := strings.Cut(env, "=")
		// Replace $ENV with the value of the environment variable using envRegex.
		v = envRegex.ReplaceAllStringFunc(v, func(s string) string {
			prefix := s[0]
			v := os.Getenv(s[2:])
			return string(prefix) + v
		})
		v = strings.ReplaceAll(v, "$$", "$")
		if err := os.Setenv(k, v); err != nil {
			return err
		}
		appendProfile.WriteString(fmt.Sprintf("export \"%s=%s\"\n", k, v))
	}

	if appendProfile.Len() > 0 {
		profile := path.Native.Join("/etc/profile.d", "builder.sh")
		if err := os.WriteFile(profile, []byte(appendProfile.String()), 0644); err != nil {
			return err
		}
	}

	// Start services
	for _, cmd := range cfg.ServiceCommands {
		if _, err := common.RunService(cmd); err != nil {
			return err
		}
	}

	// Run commands.
	for i, cmd := range cfg.Commands {
		// Check if this is the last command.
		if i == len(cfg.Commands)-1 {
			if cfg.OutputFilename == "/init/changed.archive" {
				// take a snapshot of the filesystem and store it locally.

				if err := builder.writeChangeTracker("/init.changed"); err != nil {
					return err
				}
			}
		}

		slog.Debug("running", "cmd", cmd)
		if cmd == "interactive" {
			realStart := START_TIME
			realStartTime := os.Getenv("TINYRANGE_START_TIME")
			if realStartTime != "" {
				realStartUnixMicro, err := strconv.ParseInt(realStartTime, 10, 64)
				if err == nil {
					realStart = time.UnixMicro(realStartUnixMicro)
				}
			}
			uptime, _ := GetUptime()
			slog.Debug("time to interactive", "time", time.Since(realStart), "uptime", uptime)
		}

		if err := common.RunCommand(cmd); err != nil {
			return err
		}
	}

	if cfg.ExecInit != "" {
		slog.Debug("executing init", "cmd", cfg.ExecInit, "pid", os.Getpid())
		if os.Getpid() == 1 {
			// Fork a server just running SSH.
			if err := builder.forkSSHServer(); err != nil {
				return err
			}

			return unix.Exec(cfg.ExecInit, []string{cfg.ExecInit}, os.Environ())
		} else {
			return fmt.Errorf("cannot run init command as pid is not 1")
		}
	}

	if cfg.OutputFilename == "/init/changed.archive" {
		if err := builder.uploadChangedArchive(cfg.HostAddress, "/init.changed"); err != nil {
			return err
		}
	} else if cfg.OutputFilename != "" {
		if err := builder.uploadFile(cfg.HostAddress, cfg.OutputFilename); err != nil {
			return err
		}
	}

	// Sync the filesystem.
	unix.Sync()

	return nil
}

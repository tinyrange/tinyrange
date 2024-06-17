package emulator

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"strings"

	"github.com/anmitsu/go-shlex"
	"github.com/tinyrange/pkg2/v2/emulator/common"
	"github.com/tinyrange/pkg2/v2/emulator/programs"
	"github.com/tinyrange/pkg2/v2/filesystem"
	"go.starlark.net/starlark"
)

type process struct {
	emu     *Emulator
	cwd     string
	env     map[string]string
	program common.Program
	stdin   io.ReadCloser
	stdout  io.WriteCloser
	stderr  io.WriteCloser
}

// Environ implements common.Process.
func (proc *process) Environ() map[string]string {
	ret := map[string]string{}

	for k, v := range proc.env {
		ret[k] = v
	}

	return ret
}

// Getwd implements common.Process.
func (proc *process) Getwd() string { return proc.cwd }
func (proc *process) Getenv(name string) string {
	if name == "PWD" {
		return proc.cwd
	}

	return proc.env[name]
}
func (proc *process) Stdin() io.ReadCloser   { return proc.stdin }
func (proc *process) Stdout() io.WriteCloser { return proc.stdout }
func (proc *process) Stderr() io.WriteCloser { return proc.stderr }

// Open implements common.Process.
func (proc *process) Open(filename string) (io.ReadCloser, error) {
	joined := path.Clean(filename)
	if !strings.HasPrefix(filename, "/") {
		joined = path.Join(proc.cwd, filename)
	}

	ent, err := filesystem.OpenPath(proc.emu.root, joined)
	if err != nil {
		return nil, err
	}

	return ent.File.Open()
}

// Stat implements common.Process.
func (proc *process) Stat(filename string) (fs.FileInfo, error) {
	joined := path.Clean(filename)
	if !strings.HasPrefix(filename, "/") {
		joined = path.Join(proc.cwd, filename)
	}

	ent, err := filesystem.OpenPath(proc.emu.root, joined)
	if err != nil {
		return nil, err
	}

	return ent.File.Stat()
}

// Spawn implements common.Process.
func (proc *process) Spawn(cwd string, argv []string, envp map[string]string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	child, err := proc.emu.spawn(cwd, envp, proc.stdin, proc.stdout, proc.stderr)
	if err != nil {
		return err
	}

	if err := child.exec(argv); err != nil {
		return err
	}

	return nil
}

// Emulator implements common.Process.
func (proc *process) Emulator() common.Emulator {
	return proc.emu
}

func (proc *process) exec(argv []string) error {
	// Replace the current program with a new program.
	if proc.program != nil {
		return fmt.Errorf("proc.program != nil")
	}

	f, err := proc.emu.findPath(proc, argv[0])
	if err != nil {
		return err
	}

	if prog, ok := f.(common.Program); ok {
		return prog.Run(proc, argv)
	} else {
		// assume the program is a regular file and search for a shabang on the first line.
		fh, err := f.Open()
		if err != nil {
			return err
		}
		defer fh.Close()

		// Read the first 4096 bytes
		buf := make([]byte, 4096)
		if _, err := fh.Read(buf); err != nil {
			return err
		}

		// Check for the shabang in the first 2 chars.
		if bytes.Equal(buf[:2], []byte("#!")) {
			// Get the first line.
			firstLine, _, _ := strings.Cut(string(buf), "\n")

			firstLine = strings.TrimPrefix(firstLine, "#!")

			firstLine = strings.Trim(firstLine, " ")

			args, err := shlex.Split(firstLine, true)
			if err != nil {
				return err
			}

			return proc.exec(append(args, argv...))
		} else if bytes.Equal(buf[:4], []byte("\x00asm")) {
			// Assume wasm.
			prog := NewWasmProgram(f)

			return prog.Run(proc, argv)
		} else {
			// This is probably the point to add ELF support (haha, somehow).
			return fmt.Errorf("unknown executable format for %+v", f)
		}
	}
}

var (
	_ common.Process = &process{}
)

type Emulator struct {
	root filesystem.Directory

	processes map[int]*process

	lastPid int
}

// AddFile implements common.Emulator.
func (emu *Emulator) AddFile(name string, f filesystem.File) error {
	return filesystem.CreateChild(emu.root, name, f)
}

func (emu *Emulator) findPath(proc *process, arg0 string) (filesystem.File, error) {
	// first check if the file literally exists.
	f, err := filesystem.OpenPath(emu.root, arg0)
	if err == nil {
		return f.File, nil
	} else if err != fs.ErrNotExist {
		return nil, err
	}

	// second check if the file exists under the current working directory.
	search := path.Join(proc.cwd, arg0)
	slog.Info("findPath", "search", search, "arg0", arg0)
	f, err = filesystem.OpenPath(emu.root, search)
	if err == nil {
		return f.File, nil
	} else if err != fs.ErrNotExist {
		return nil, err
	}

	if strings.Contains(arg0, "/") {
		return nil, fs.ErrNotExist
	}

	// Third get the PATH variable and check each of the items.
	pathTokens := strings.Split(proc.Getenv("PATH"), ":")

	for _, option := range pathTokens {
		search := path.Join(option, arg0)

		slog.Info("findPath", "search", search)

		f, err = filesystem.OpenPath(emu.root, search)
		if err == nil {
			return f.File, nil
		} else if err != fs.ErrNotExist {
			return nil, err
		}
	}

	return nil, fs.ErrNotExist
}

func (emu *Emulator) spawn(cwd string, envp map[string]string, stdin io.ReadCloser, stdout io.WriteCloser, stderr io.WriteCloser) (*process, error) {
	proc := &process{
		emu:    emu,
		cwd:    cwd,
		stdin:  stdin,
		stdout: stdout,
		stderr: stderr,
		env:    envp,
	}

	if proc.env == nil {
		proc.env = map[string]string{}
	}

	emu.lastPid += 1

	emu.processes[emu.lastPid] = proc

	return proc, nil
}

type RunOptions struct {
	Command          string
	WorkingDirectory string
	Environment      map[string]string
}

func (emu *Emulator) Run(opts RunOptions) error {
	args, err := shlex.Split(opts.Command, true)
	if err != nil {
		return err
	}

	proc, err := emu.spawn(opts.WorkingDirectory, opts.Environment, os.Stdin, os.Stdout, os.Stderr)
	if err != nil {
		return err
	}

	if err := proc.exec(args); err != nil {
		return err
	}

	return nil
}

// Attr implements starlark.HasAttrs.
func (emu *Emulator) Attr(name string) (starlark.Value, error) {
	if name == "run" {
		return starlark.NewBuiltin("Emulator.run", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				command string
				cwd     string
				env     *starlark.Dict
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"command?", &command,
				"cwd?", &cwd,
				"env?", &env,
			); err != nil {
				return starlark.None, err
			}

			if cwd == "" {
				cwd = "/"
			}

			cmdEnv := map[string]string{}

			if env != nil {
				for _, key := range env.Keys() {
					value, _, err := env.Get(key)
					if err != nil {
						return starlark.None, err
					}

					keyString, ok := starlark.AsString(key)
					if !ok {
						return starlark.None, fmt.Errorf("expected string got %s", key.Type())
					}

					valueString, ok := starlark.AsString(value)
					if !ok {
						return starlark.None, fmt.Errorf("expected string got %s", key.Type())
					}

					cmdEnv[keyString] = valueString
				}
			}

			if err := emu.Run(RunOptions{Command: command, WorkingDirectory: cwd, Environment: cmdEnv}); err != nil {
				return starlark.None, err
			}

			return starlark.None, nil
		}), nil
	} else if name == "add_shell_utilities" {
		return starlark.NewBuiltin("Emulator.add_shell_utilities", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			return starlark.None, programs.AddShellUtilities(emu)
		}), nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (f *Emulator) AttrNames() []string {
	return []string{"run", "add_shell_utilities"}
}

func (f *Emulator) String() string      { return "Emulator" }
func (*Emulator) Type() string          { return "Emulator" }
func (*Emulator) Hash() (uint32, error) { return 0, fmt.Errorf("Emulator is not hashable") }
func (*Emulator) Truth() starlark.Bool  { return starlark.True }
func (*Emulator) Freeze()               {}

var (
	_ starlark.Value    = &Emulator{}
	_ starlark.HasAttrs = &Emulator{}
	_ common.Emulator   = &Emulator{}
)

func New(fs filesystem.Directory) *Emulator {
	return &Emulator{root: fs, processes: make(map[int]*process)}
}

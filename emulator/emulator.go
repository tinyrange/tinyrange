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
	"github.com/tinyrange/pkg2/v2/filesystem"
	"go.starlark.net/starlark"
)

type process struct {
	emu     *Emulator
	cwd     string
	env     map[string]string
	program Program
	stdin   io.ReadCloser
	stdout  io.WriteCloser
	stderr  io.WriteCloser
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

	if prog, ok := f.(Program); ok {
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
		if !bytes.Equal(buf[:2], []byte("#!")) {
			// This is probably the point to add ELF support (haha, somehow).
			return fmt.Errorf("unknown executable format for %+v", f)
		}

		// Get the first line.
		firstLine, _, _ := strings.Cut(string(buf), "\n")

		firstLine = strings.TrimPrefix(firstLine, "#!")

		firstLine = strings.Trim(firstLine, " ")

		args, err := shlex.Split(firstLine, true)
		if err != nil {
			return err
		}

		return proc.exec(append(args, argv...))
	}
}

var (
	_ Process = &process{}
)

type Emulator struct {
	root filesystem.Directory

	processes map[int]*process

	lastPid int
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

	return nil, fmt.Errorf("not implemented")
}

func (emu *Emulator) spawn(cwd string, stdin io.ReadCloser, stdout io.WriteCloser, stderr io.WriteCloser) (*process, error) {
	proc := &process{
		emu:    emu,
		cwd:    cwd,
		stdin:  stdin,
		stdout: stdout,
		stderr: stderr,
		env:    make(map[string]string),
	}

	emu.lastPid += 1

	emu.processes[emu.lastPid] = proc

	return proc, nil
}

type RunOptions struct {
	Command          string
	WorkingDirectory string
}

func (emu *Emulator) Run(opts RunOptions) error {
	args, err := shlex.Split(opts.Command, true)
	if err != nil {
		return err
	}

	proc, err := emu.spawn(opts.WorkingDirectory, os.Stdin, os.Stdout, os.Stderr)
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
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"command?", &command,
				"cwd?", &cwd,
			); err != nil {
				return starlark.None, err
			}

			if cwd == "" {
				cwd = "/"
			}

			if err := emu.Run(RunOptions{Command: command, WorkingDirectory: cwd}); err != nil {
				return starlark.None, err
			}

			return starlark.None, nil
		}), nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (f *Emulator) AttrNames() []string {
	return []string{"run"}
}

func (f *Emulator) String() string      { return "Emulator" }
func (*Emulator) Type() string          { return "Emulator" }
func (*Emulator) Hash() (uint32, error) { return 0, fmt.Errorf("Emulator is not hashable") }
func (*Emulator) Truth() starlark.Bool  { return starlark.True }
func (*Emulator) Freeze()               {}

var (
	_ starlark.Value    = &Emulator{}
	_ starlark.HasAttrs = &Emulator{}
)

func New(fs filesystem.Directory) *Emulator {
	return &Emulator{root: fs, processes: make(map[int]*process)}
}

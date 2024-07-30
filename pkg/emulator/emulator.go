package emulator

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"path"
	"strings"

	"github.com/anmitsu/go-shlex"
	"github.com/tinyrange/tinyrange/pkg/emulator/programs/shell"
	"github.com/tinyrange/tinyrange/pkg/emulator/shared"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"go.starlark.net/starlark"
)

type starProgram struct {
	filesystem.File

	run starlark.Callable
}

// Create implements shared.Program.
func (s *starProgram) Create() shared.Program { return s }

// Run implements shared.Program.
func (s *starProgram) Run(proc shared.Process, argv []string) error {
	p := proc.(*process)

	var args []starlark.Value

	for _, arg := range argv {
		args = append(args, starlark.String(arg))
	}

	ret, err := starlark.Call(
		&starlark.Thread{Name: p.kernel.scriptFilename},
		s.run,
		starlark.Tuple{p, starlark.NewList(args)},
		[]starlark.Tuple{},
	)
	if err != nil {
		if sErr, ok := err.(*starlark.EvalError); ok {
			slog.Error("got starlark error", "error", sErr, "backtrace", sErr.Backtrace())
		}
		return err
	}

	_ = ret

	return nil
}

var (
	_ shared.Program = &starProgram{}
)

type simpleProgram struct {
	filesystem.File

	run func(proc shared.Process, argv []string) error
}

// implements shared.Program.
func (s *simpleProgram) Create() shared.Program                       { return s }
func (s *simpleProgram) Run(proc shared.Process, argv []string) error { return s.run(proc, argv) }

var (
	_ shared.Program = &simpleProgram{}
)

type process struct {
	id     int
	cwd    string
	env    shared.Environment
	kernel *Emulator
	prog   shared.Program

	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
}

// Get implements starlark.HasSetKey.
func (proc *process) Get(k starlark.Value) (v starlark.Value, found bool, err error) {
	filename, ok := starlark.AsString(k)
	if !ok {
		return starlark.None, false, fmt.Errorf("could not convert %s to string", k.Type())
	}

	p := proc.Resolve(filename)

	f, err := proc.kernel.openPath(p)
	if errors.Is(err, fs.ErrNotExist) {
		return starlark.None, false, nil
	} else if err != nil {
		return starlark.None, false, err
	}

	return filesystem.NewStarFile(f, p), true, nil
}

// SetKey implements starlark.HasSetKey.
func (proc *process) SetKey(k starlark.Value, v starlark.Value) error {
	filename, ok := starlark.AsString(k)
	if !ok {
		return fmt.Errorf("could not convert %s to string", k.Type())
	}

	p := proc.Resolve(filename)

	root := proc.kernel.Root()

	file, err := filesystem.AsFile(v)
	if err != nil {
		return err
	}

	return filesystem.CreateChild(root, p, file)
}

// Chdir implements shared.Process.
func (proc *process) Chdir(name string) error {
	// slog.Info("chdir", "name", name)

	proc.cwd = path.Join(proc.cwd, name)

	return nil
}

// Read implements shared.Process.
func (proc *process) Read(p []byte) (n int, err error) {
	return proc.stdin.Read(p)
}

// implements shared.Process.
func (p *process) SetStderr(out io.Writer) { p.stderr = out }
func (p *process) SetStdin(in io.Reader)   { p.stdin = in }
func (p *process) SetStdout(out io.Writer) { p.stdout = out }

// Stderr implements shared.Process.
func (p *process) Stderr() io.Writer { return p.stderr }

// Write implements shared.Process.
func (proc *process) Write(p []byte) (n int, err error) { return proc.stdout.Write(p) }

// Fork implements shared.Process.
func (p *process) Fork() (shared.Process, error) {
	proc, err := p.kernel.NewProcess()
	if err != nil {
		return nil, err
	}

	new := proc.(*process)

	new.cwd = p.cwd
	new.env = p.env.Clone()
	new.stdout = p.stdout
	new.stderr = p.stderr
	new.stdin = p.stdin

	return new, nil
}

// Attr implements starlark.HasAttrs.
func (p *process) Attr(name string) (starlark.Value, error) {
	if name == "write" {
		return starlark.NewBuiltin("Process.write", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				contents string
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"contents", &contents,
			); err != nil {
				return starlark.None, err
			}

			if _, err := p.stdout.Write([]byte(contents)); err != nil {
				return starlark.None, err
			}

			return starlark.None, nil
		}), nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (p *process) AttrNames() []string {
	return []string{"write"}
}

// SetEnv implements shared.Process.
func (p *process) Setenv(key string, value string) {
	p.env.Set(key, value)
}

// GetEnv implements shared.Process.
func (p *process) Getenv(key string) string {
	return p.env.Get(key)
}

func (p *process) Resolve(name string) string {
	if strings.HasPrefix(name, "/") {
		return name
	} else {
		return path.Join(p.cwd, name)
	}
}

// Open implements shared.Process.
func (p *process) Open(name string) (filesystem.FileHandle, error) {
	name = p.Resolve(name)

	f, err := p.kernel.openPath(name)
	if err != nil {
		return nil, fmt.Errorf("failed to open %s: %s", name, err)
	}

	return f.Open()
}

// LookPath implements shared.Process.
func (p *process) LookPath(name string) (string, error) {
	return p.kernel.LookPath(p.cwd, p.env, name)
}

// Exec implements shared.Process.
func (p *process) Exec(args []string) error {
	name, err := p.LookPath(args[0])
	if err != nil {
		return err
	}

	// slog.Info("found executable", "name", name)

	prog, extra, err := p.kernel.LookupExecutable(name)
	if err != nil {
		return err
	}

	args = append(extra, args[1:]...)

	p.prog = prog.Create()

	return p.prog.Run(p, args)
}

// Kernel implements shared.Process.
func (p *process) Kernel() shared.Kernel {
	return p.kernel
}

func (*process) String() string        { return "Process" }
func (*process) Type() string          { return "Process" }
func (*process) Hash() (uint32, error) { return 0, fmt.Errorf("Process is not hashable") }
func (*process) Truth() starlark.Bool  { return starlark.True }
func (*process) Freeze()               {}

var (
	_ shared.Process     = &process{}
	_ starlark.Value     = &process{}
	_ starlark.HasAttrs  = &process{}
	_ starlark.HasSetKey = &process{}
)

type Emulator struct {
	scriptFilename string
	root           filesystem.Directory
	processes      map[int]*process
	nextProcessId  int
}

func (emu *Emulator) openPath(name string) (filesystem.File, error) {
	f, err := filesystem.OpenPath(emu.root, name)
	if err != nil {
		return nil, err
	}

	return f.File, nil
}

// LookPath implements shared.Kernel.
func (emu *Emulator) LookPath(cwd string, env shared.Environment, name string) (string, error) {
	if strings.HasPrefix(name, "./") {
		name = path.Join(cwd, name)
		if _, err := emu.openPath(name); err != nil {
			return "", fmt.Errorf("could not open %s: %s", name, err)
		}

		return name, nil
	}

	if _, err := emu.openPath(name); err == nil {
		return name, nil
	}

	pathOptions := strings.Split(env.Get("PATH"), ":")

	for _, opt := range pathOptions {
		if _, err := emu.openPath(path.Join(opt, name)); err == nil {
			return path.Join(opt, name), nil
		}
	}

	return "", fmt.Errorf("could not find: %s", name)
}

// LookPath implements shared.Kernel.
func (emu *Emulator) LookupExecutable(name string) (shared.Program, []string, error) {
	file, err := emu.openPath(name)
	if err != nil {
		return nil, nil, fmt.Errorf("could not open %s: %s", name, err)
	}

	if prog, ok := file.(shared.Program); ok {
		return prog, []string{name}, nil
	}

	// read the first line looking for a interpreter directive.
	fh, err := file.Open()
	if err != nil {
		return nil, nil, err
	}
	defer fh.Close()

	scanner := bufio.NewScanner(fh)

	scanner.Scan()

	line := scanner.Text()

	if strings.HasPrefix(line, "#!") {
		tokens, err := shlex.Split(strings.TrimPrefix(line, "#!"), true)
		if err != nil {
			return nil, nil, err
		}

		prog, first, err := emu.LookupExecutable(tokens[0])
		if err != nil {
			return nil, nil, err
		}

		return prog, append(append(first, tokens[1:]...), name), nil
	}

	return nil, nil, fmt.Errorf("could not find program for: %s", name)
}

func (emu *Emulator) AddSimpleProgram(filename string, prog func(proc shared.Process, argv []string) error) error {
	return emu.AddProgram(filename, &simpleProgram{
		File: filesystem.NewMemoryFile(filesystem.TypeRegular),
		run:  prog,
	})
}

func (emu *Emulator) AddProgram(filename string, prog shared.Program) error {
	return filesystem.CreateChild(emu.root, filename, prog)
}

func (emu *Emulator) NewProcess() (shared.Process, error) {
	proc := &process{
		id:     emu.nextProcessId,
		kernel: emu,
		cwd:    "/",
		env:    make(shared.Environment),
		stdin:  bytes.NewReader([]byte{}),
		stdout: new(bytes.Buffer),
		stderr: new(bytes.Buffer),
	}

	proc.env.Set("PATH", "/usr/local/bin:/usr/bin:/bin:/usr/local/sbin:/usr/sbin:/sbin")

	emu.processes[proc.id] = proc

	emu.nextProcessId += 1

	return proc, nil
}

func (emu *Emulator) Root() filesystem.Directory {
	return emu.root
}

func (emu *Emulator) AddBuiltinPrograms() error {
	if err := emu.AddSimpleProgram("/usr/bin/env", func(proc shared.Process, argv []string) error {
		name, err := proc.LookPath(argv[1])
		if err != nil {
			return err
		}

		return proc.Exec(append([]string{name}, argv[2:]...))
	}); err != nil {
		return err
	}

	if err := emu.AddProgram("/bin/bash", shell.NewShellProgram()); err != nil {
		return err
	}

	if err := emu.AddProgram("/bin/sh", shell.NewShellProgram()); err != nil {
		return err
	}

	return nil
}

func (emu *Emulator) RunShell(command string) error {
	proc, err := emu.NewProcess()
	if err != nil {
		return err
	}

	if err := proc.Exec([]string{"/bin/sh", "-c", command}); err != nil {
		return err
	}

	return nil
}

// Attr implements starlark.HasAttrs.
func (emu *Emulator) Attr(name string) (starlark.Value, error) {
	if name == "add_command" {
		return starlark.NewBuiltin("Emulator.add_command", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				filename string
				run      starlark.Callable
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"filename", &filename,
				"run", &run,
			); err != nil {
				return starlark.None, err
			}

			if err := emu.AddProgram(filename, &starProgram{
				File: filesystem.NewMemoryFile(filesystem.TypeRegular),
				run:  run,
			}); err != nil {
				return starlark.None, err
			}

			return starlark.None, nil
		}), nil
	}
	return nil, nil
}

// AttrNames implements starlark.HasAttrs.
func (emu *Emulator) AttrNames() []string {
	return []string{"add_command"}
}

func (*Emulator) String() string        { return "Emulator" }
func (*Emulator) Type() string          { return "Emulator" }
func (*Emulator) Hash() (uint32, error) { return 0, fmt.Errorf("Emulator is not hashable") }
func (*Emulator) Truth() starlark.Bool  { return starlark.True }
func (*Emulator) Freeze()               {}

var (
	_ starlark.Value    = &Emulator{}
	_ starlark.HasAttrs = &Emulator{}
	_ shared.Kernel     = &Emulator{}
)

func New(scriptFilename string, root filesystem.Directory) *Emulator {
	return &Emulator{
		scriptFilename: scriptFilename,
		root:           root,
		processes:      make(map[int]*process),
	}
}

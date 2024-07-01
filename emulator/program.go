package emulator

import (
	"fmt"
	"log/slog"

	"github.com/tinyrange/tinyrange/emulator/common"
	"github.com/tinyrange/tinyrange/filesystem"
	"go.starlark.net/starlark"
)

type starProcess struct {
	proc common.Process
}

func (f *starProcess) String() string      { return "Process" }
func (*starProcess) Type() string          { return "Process" }
func (*starProcess) Hash() (uint32, error) { return 0, fmt.Errorf("Process is not hashable") }
func (*starProcess) Truth() starlark.Bool  { return starlark.True }
func (*starProcess) Freeze()               {}

var (
	_ starlark.Value = &starProcess{}
)

type starProgram struct {
	filesystem.File

	name string

	callable starlark.Callable
}

// Name implements Program.
func (s *starProgram) Name() string { return s.name }

// Run implements Program.
func (s *starProgram) Run(proc common.Process, argv []string) error {
	thread := &starlark.Thread{Name: s.name}

	process := &starProcess{proc: proc}

	var args starlark.Tuple
	for _, arg := range argv {
		args = append(args, starlark.String(arg))
	}

	res, err := starlark.Call(thread, s.callable, starlark.Tuple{process, args}, []starlark.Tuple{})
	if err != nil {
		if sErr, ok := err.(*starlark.EvalError); ok {
			slog.Error("got starlark error", "error", sErr, "backtrace", sErr.Backtrace())
		}
		return err
	}

	_ = res

	return nil
}

func (f *starProgram) String() string      { return fmt.Sprintf("Program{%s}", f.Name()) }
func (*starProgram) Type() string          { return "Program" }
func (*starProgram) Hash() (uint32, error) { return 0, fmt.Errorf("Program is not hashable") }
func (*starProgram) Truth() starlark.Bool  { return starlark.True }
func (*starProgram) Freeze()               {}

var (
	_ common.Program = &starProgram{}
	_ starlark.Value = &starProgram{}
)

func NewStarProgram(name string, entry starlark.Callable) (starlark.Value, error) {
	return &starProgram{name: name, callable: entry, File: filesystem.NewMemoryFile()}, nil
}

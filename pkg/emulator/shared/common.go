package shared

import (
	"io"

	"github.com/tinyrange/tinyrange/pkg/filesystem"
)

type Environment map[string]string

func (env Environment) Set(key string, value string) {
	env[key] = value
}

func (env Environment) Get(key string) string {
	return env[key]
}

func (env Environment) Has(key string) bool {
	_, ok := env[key]
	return ok
}

func (env Environment) Clone() Environment {
	ret := make(Environment)

	for k, v := range env {
		ret[k] = v
	}

	return ret
}

type Kernel interface {
	LookPath(cwd string, env Environment, name string) (string, error)
	LookupExecutable(name string) (Program, []string, error)
}

type Process interface {
	io.Writer
	io.Reader

	Kernel() Kernel
	Fork() (Process, error)
	Exec(args []string) error
	LookPath(name string) (string, error)
	Open(name string) (filesystem.FileHandle, error)
	Chdir(name string) error
	Setenv(key string, value string)
	Getenv(key string) string

	Stderr() io.Writer

	SetStdin(in io.Reader)
	SetStdout(out io.Writer)
	SetStderr(out io.Writer)
}

type Program interface {
	filesystem.File

	Create() Program
	Run(proc Process, argv []string) error
}

package common

import (
	"io"
	"io/fs"

	"github.com/tinyrange/tinyrange/filesystem"
)

type Emulator interface {
	AddFile(name string, f filesystem.File) error
}

type Process interface {
	Emulator() Emulator

	Spawn(cwd string, argv []string, envp map[string]string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error

	Open(filename string) (io.ReadCloser, error)
	Stat(filename string) (fs.FileInfo, error)

	Stdin() io.ReadCloser
	Stdout() io.WriteCloser
	Stderr() io.WriteCloser

	Getenv(name string) string
	Getwd() string

	Environ() map[string]string
}

type Program interface {
	filesystem.File
	Name() string
	Run(proc Process, argv []string) error
}

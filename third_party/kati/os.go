package kati

import (
	"io"
	"io/fs"
)

type File interface {
	io.WriteCloser

	Chmod(mode fs.FileMode) error
}

type EnvironmentInterface interface {
	ReadFile(filename string) ([]byte, error)
	Exec(args []string) ([]byte, error)
	Stat(filename string) (fs.FileInfo, error)
	Setenv(key string, value string)
	Unsetenv(key string)
	Create(filename string) (File, error)
	NumCPU() int
	Abspath(p string) (string, error)
	EvalSymlinks(p string) (string, error)
}

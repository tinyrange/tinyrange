package kati

import (
	"io"
	"io/fs"
)

type File interface {
	io.ReadWriteCloser

	Chmod(mode fs.FileMode) error
	Readdirnames(limit int) ([]string, error)
}

type EnvironmentInterface interface {
	ReadFile(filename string) ([]byte, error)
	Exec(args []string) ([]byte, error)
	Stat(filename string) (fs.FileInfo, error)
	Lstat(filename string) (fs.FileInfo, error)

	Setenv(key string, value string)
	Unsetenv(key string)

	Create(filename string) (File, error)
	Open(filename string) (File, error)
	Remove(filename string) error

	NumCPU() int

	Abspath(p string) (string, error)
	EvalSymlinks(p string) (string, error)
}

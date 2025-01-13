package build2

import "io"

type Color int

const (
	ColorDefault Color = iota
	ColorRed
	ColorGreen
	ColorYellow
	ColorBlue
	ColorGrey
)

type Logger interface {
	io.Closer

	Logf(format string, args ...interface{})
	Describe(color Color, format string, args ...interface{})
	Child(description string) Logger
}

type RootLogger interface {
	io.Closer
	Run(w io.Writer) error
	Group(name string) Logger
}

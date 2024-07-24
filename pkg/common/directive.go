package common

import (
	"fmt"

	"go.starlark.net/starlark"
)

type Directive interface {
	MarshallableObject
}

type DirectiveFactory interface {
	AsDirective() (Directive, error)
}

type DirectiveRunCommand string

// TagMarshallableObject implements Directive.
func (d DirectiveRunCommand) TagMarshallableObject() { panic("unimplemented") }

type DirectiveAddFile struct {
	Filename   string
	Definition BuildDefinition
	Contents   []byte
	Executable bool
}

// TagMarshallableObject implements Directive.
func (d DirectiveAddFile) TagMarshallableObject() { panic("unimplemented") }

type DirectiveArchive struct {
	Definition BuildDefinition
	Target     string
}

// TagMarshallableObject implements Directive.
func (d DirectiveArchive) TagMarshallableObject() { panic("unimplemented") }

var (
	_ Directive = DirectiveRunCommand("")
	_ Directive = DirectiveAddFile{}
	_ Directive = DirectiveArchive{}
)

type StarDirective struct {
	Directive
}

func (d *StarDirective) String() string      { return "Directive" }
func (d *StarDirective) Type() string        { return fmt.Sprintf("%T", d.Directive) }
func (*StarDirective) Hash() (uint32, error) { return 0, fmt.Errorf("Directive is not hashable") }
func (*StarDirective) Truth() starlark.Bool  { return starlark.True }
func (*StarDirective) Freeze()               {}

var (
	_ starlark.Value = &StarDirective{}
)

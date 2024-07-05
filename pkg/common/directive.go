package common

import (
	"fmt"
	"strings"

	"go.starlark.net/starlark"
)

type Directive interface {
	Tag() string
}

type DirectiveFactory interface {
	AsDirective() (Directive, error)
}

type DirectiveRunCommand string

// Tag implements Directive.
func (d DirectiveRunCommand) Tag() string {
	return fmt.Sprintf("RunCommand_%s", strings.ReplaceAll(string(d), " ", "_"))
}

type DirectiveAddFile struct {
	Filename   string
	Contents   []byte
	Executable bool
}

// Tag implements Directive.
func (d DirectiveAddFile) Tag() string {
	sum := GetSha256Hash(d.Contents)

	return fmt.Sprintf("AddFile_%s_%s_%+v", d.Filename, sum, d.Executable)
}

type DirectiveArchive struct {
	Definition BuildDefinition
	Target     string
}

// Tag implements Directive.
func (d DirectiveArchive) Tag() string {
	return fmt.Sprintf("DirArchive_%s_%s", d.Definition.Tag(), d.Target)
}

var (
	_ Directive = DirectiveRunCommand("")
	_ Directive = DirectiveAddFile{}
	_ Directive = DirectiveArchive{}
)

type StarDirective struct {
	Directive
}

func (d *StarDirective) String() string      { return d.Tag() }
func (d *StarDirective) Type() string        { return fmt.Sprintf("%T", d.Directive) }
func (*StarDirective) Hash() (uint32, error) { return 0, fmt.Errorf("Directive is not hashable") }
func (*StarDirective) Truth() starlark.Bool  { return starlark.True }
func (*StarDirective) Freeze()               {}

var (
	_ starlark.Value = &StarDirective{}
)

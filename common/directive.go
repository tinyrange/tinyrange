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

type DirectiveBaseImage string

// Tag implements Directive.
func (d DirectiveBaseImage) Tag() string {
	return fmt.Sprintf("BaseImage_%s", d)
}

type DirectiveRunCommand string

// Tag implements Directive.
func (d DirectiveRunCommand) Tag() string {
	return fmt.Sprintf("RunCommand_%s", strings.ReplaceAll(string(d), " ", "_"))
}

var (
	_ Directive = DirectiveBaseImage("")
	_ Directive = DirectiveRunCommand("")
)

type StarDirective struct {
	Directive
}

func (*StarDirective) String() string        { return "Directive" }
func (*StarDirective) Type() string          { return "Directive" }
func (*StarDirective) Hash() (uint32, error) { return 0, fmt.Errorf("Directive is not hashable") }
func (*StarDirective) Truth() starlark.Bool  { return starlark.True }
func (*StarDirective) Freeze()               {}

var (
	_ starlark.Value = &StarDirective{}
)

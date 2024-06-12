package common

import (
	"fmt"

	"go.starlark.net/starlark"
)

type Directive interface {
	tagDirective()
}

type DirectiveBaseImage string

// tagDirective implements Directive.
func (d DirectiveBaseImage) tagDirective() { panic("unimplemented") }

type DirectiveRunCommand string

// tagDirective implements Directive.
func (d DirectiveRunCommand) tagDirective() { panic("unimplemented") }

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

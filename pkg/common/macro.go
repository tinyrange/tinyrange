package common

import (
	"fmt"
	"strings"

	"go.starlark.net/starlark"
)

type MacroContext interface {
	Thread() *starlark.Thread
}

type Macro interface {
	Call(ctx MacroContext) (BuildDefinition, error)
}

type DefinitionMacro struct {
	BuildDefinition
}

// Call implements Macro.
func (d DefinitionMacro) Call(ctx MacroContext) (BuildDefinition, error) {
	return d.BuildDefinition, nil
}

var (
	_ Macro = DefinitionMacro{}
)

type StarlarkMacroArgument interface {
	Value(ctx MacroContext) (starlark.Value, error)
}

type StarlarkMacroString string

// Value implements StarlarkMacroArgument.
func (s StarlarkMacroString) Value(ctx MacroContext) (starlark.Value, error) {
	return starlark.String(s), nil
}

var (
	_ StarlarkMacroArgument = StarlarkMacroString("")
)

type StarlarkMacro struct {
	target *starlark.Function
	args   []StarlarkMacroArgument
}

// Call implements Macro.
func (s *StarlarkMacro) Call(ctx MacroContext) (BuildDefinition, error) {
	var args []starlark.Value

	for _, arg := range s.args {
		val, err := arg.Value(ctx)
		if err != nil {
			return nil, err
		}

		args = append(args, val)
	}

	ret, err := starlark.Call(ctx.Thread(), s.target, args, nil)
	if err != nil {
		return nil, err
	}

	if ret == starlark.None {
		return nil, nil
	}

	def, ok := ret.(BuildDefinition)
	if !ok {
		return nil, fmt.Errorf("could not convert %s to BuildDefinition", ret.Type())
	}

	return def, nil
}

var (
	_ Macro = &StarlarkMacro{}
)

func parseMacroArgument(desc string, args []string) (StarlarkMacroArgument, []string, error) {
	tokens := strings.Split(desc, ":")
	name := tokens[0]
	typ := tokens[1]

	_ = name

	switch typ {
	case "string":
		val := args[0]

		return StarlarkMacroString(val), args[1:], nil
	default:
		return nil, nil, fmt.Errorf("unknown macro argument type: %s", typ)
	}
}

func ParseMacro(ctx MacroContext, f *starlark.Function, args []string) (Macro, error) {
	var err error

	doc := f.Doc()

	if !strings.HasPrefix(doc, "#macro ") {
		return nil, fmt.Errorf("function is not a macro: %s", f.Name())
	}

	ret := &StarlarkMacro{target: f}

	argDefs := strings.Split(doc, " ")[1:]

	for _, arg := range argDefs {
		var argVal StarlarkMacroArgument

		argVal, args, err = parseMacroArgument(arg, args)
		if err != nil {
			return nil, err
		}

		ret.args = append(ret.args, argVal)
	}

	return ret, nil
}

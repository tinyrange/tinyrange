package macro

import (
	"fmt"
	"strings"

	"github.com/tinyrange/tinyrange/pkg/common"
	"go.starlark.net/starlark"
)

type MacroContext interface {
	Thread() *starlark.Thread
	Builder(name string) (common.InstallationPlanBuilder, error)
	AddBuilder(name string, builder common.InstallationPlanBuilder)
}

type Macro interface {
	Call(ctx MacroContext) (common.MacroResult, error)
}

type DefinitionMacro struct {
	common.BuildDefinition
}

// Call implements Macro.
func (d DefinitionMacro) Call(ctx MacroContext) (common.MacroResult, error) {
	return d.BuildDefinition, nil
}

type DirectiveMacro struct {
	common.Directive
}

// Call implements Macro.
func (d DirectiveMacro) Call(ctx MacroContext) (common.MacroResult, error) {
	return d.Directive, nil
}

var (
	_ Macro = DefinitionMacro{}
	_ Macro = DirectiveMacro{}
)

type StarlarkMacroArgument interface {
	Value(ctx MacroContext) (starlark.Value, error)
}

type StarlarkMacroString string

// Value implements StarlarkMacroArgument.
func (s StarlarkMacroString) Value(ctx MacroContext) (starlark.Value, error) {
	return starlark.String(s), nil
}

type StarlarkMacroBuilder string

// Value implements StarlarkMacroArgument.
func (s StarlarkMacroBuilder) Value(ctx MacroContext) (starlark.Value, error) {
	return ctx.Builder(string(s))
}

var (
	_ StarlarkMacroArgument = StarlarkMacroString("")
	_ StarlarkMacroArgument = StarlarkMacroBuilder("")
)

type StarlarkMacro struct {
	target *starlark.Function
	args   []StarlarkMacroArgument
}

// Call implements Macro.
func (s *StarlarkMacro) Call(ctx MacroContext) (common.MacroResult, error) {
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

	def, ok := ret.(common.MacroResult)
	if !ok {
		return nil, fmt.Errorf("could not convert %s to MacroResult", ret.Type())
	}

	return def, nil
}

var (
	_ Macro = &StarlarkMacro{}
)

func parseMacroArgument(desc string, args []string) (StarlarkMacroArgument, []string, error) {
	tokens := strings.Split(desc, ",")
	typ := tokens[0]

	switch typ {
	case "string":
		val := args[0]

		return StarlarkMacroString(val), args[1:], nil
	case "builder":
		name := tokens[1]

		return StarlarkMacroBuilder(name), args, nil
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

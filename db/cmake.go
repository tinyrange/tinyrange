package db

import (
	"fmt"
	"log/slog"
	"path"
	"strings"

	"github.com/kythe/llvmbzlgen/cmakelib/ast"
	"go.starlark.net/starlark"
)

type evaluator struct {
	parent *evaluator
	values *starlark.Dict
}

// Get implements ast.Bindings.
func (eval *evaluator) Get(k string) string {
	v, ok, err := eval.values.Get(starlark.String(k))
	if err != nil {
		slog.Error("CMakeEvaluator: failed to get", "k", k, "err", err)
	}

	if !ok {
		if eval.parent != nil {
			return eval.parent.Get(k)
		} else {
			return ""
		}
	}

	str, ok := starlark.AsString(v)
	if !ok {
		return v.String()
	} else {
		return str
	}
}

// GetCache implements ast.Bindings.
func (eval *evaluator) GetCache(k string) string {
	v, ok, err := eval.values.Get(starlark.String(k))
	if err != nil {
		slog.Error("CMakeEvaluator: failed to get", "k", k, "err", err)
	}

	if !ok {
		if eval.parent != nil {
			return eval.parent.GetCache(k)
		} else {
			return ""
		}
	}

	str, ok := starlark.AsString(v)
	if !ok {
		return v.String()
	} else {
		return str
	}
}

// GetEnv implements ast.Bindings.
func (eval *evaluator) GetEnv(k string) string {
	v, ok, err := eval.values.Get(starlark.String(k))
	if err != nil {
		slog.Error("CMakeEvaluator: failed to get", "k", k, "err", err)
	}

	if !ok {
		if eval.parent != nil {
			return eval.parent.GetEnv(k)
		} else {
			return ""
		}
	}

	str, ok := starlark.AsString(v)
	if !ok {
		return v.String()
	} else {
		return str
	}
}

var (
	_ ast.Bindings = &evaluator{}
)

type CMakeStatement interface {
	cmakeStatement()

	Eval(eval *CMakeEvaluatorScope) error
}

type CommandStatement struct {
	ast.CommandInvocation
}

// Eval implements CMakeStatement.
func (c *CommandStatement) Eval(eval *CMakeEvaluatorScope) error {
	args := c.Arguments.Eval(eval.evaluator)

	return eval.eval.evalCommand(eval, c.Name, args)
}

// cmakeStatement implements CMakeStatement.
func (c *CommandStatement) cmakeStatement() { panic("unimplemented") }

func (c *CommandStatement) String() string {
	return fmt.Sprintf("Command{%s}", c.Name)
}

type IfStatement struct {
	ast.CommandInvocation

	Body []CMakeStatement
	Else []CMakeStatement
}

func (i *IfStatement) evalArg(eval *CMakeEvaluatorScope, arg ast.Argument) (string, error) {
	switch {
	case arg.QuotedArgument != nil:
		return strings.Join(arg.QuotedArgument.Eval(eval.evaluator), " "), nil
	case arg.UnquotedArgument != nil:
		val := strings.Join(arg.UnquotedArgument.Eval(eval.evaluator), " ")

		return eval.evaluator.Get(val), nil
	case arg.BracketArgument != nil:
		return strings.Join(arg.BracketArgument.Eval(eval.evaluator), " "), nil
	case arg.ArgumentList != nil:
		// Include the parens, but only for nested argument lists.
		values := []string{"("}
		values = append(values, arg.ArgumentList.Eval(eval.evaluator)...)
		return strings.Join(append(values, ")"), " "), nil
	}
	panic("Missing concrete argument!")
}

func (i *IfStatement) evalCondition(eval *CMakeEvaluatorScope) (bool, error) {
	args := i.Arguments.Values
	// slog.Info("if", "args", args)

	not := false

	if args[0].Eval(eval.evaluator)[0] == "NOT" {
		not = true
		args = args[1:]
	}

	if len(args) == 3 {

		op := args[1].Eval(eval.evaluator)[0]

		_ = not

		switch op {
		case "STREQUAL":
			lhs, err := i.evalArg(eval, args[0])
			if err != nil {
				return false, err
			}

			rhs, err := i.evalArg(eval, args[2])
			if err != nil {
				return false, err
			}

			slog.Info("if", "op", op, "lhs", lhs, "rhs", rhs)

			result := lhs == rhs

			if not {
				result = !result
			}

			return result, nil
		case "VERSION_EQUAL":
			lhs, err := i.evalArg(eval, args[0])
			if err != nil {
				return false, err
			}

			rhs, err := i.evalArg(eval, args[2])
			if err != nil {
				return false, err
			}

			slog.Info("if", "op", op, "lhs", lhs, "rhs", rhs)

			result := lhs == rhs

			if not {
				result = !result
			}

			return result, nil
		default:
			return false, fmt.Errorf("if op not implemented: %s", op)
		}
	} else if len(args) == 2 {
		op := args[0].Eval(eval.evaluator)[0]

		switch op {
		case "COMMAND":
			command := args[1].Eval(eval.evaluator)[0]

			_, result := eval.eval.commands[command]

			if not {
				result = !result
			}

			return result, nil
		case "POLICY":
			policyName := args[1].Eval(eval.evaluator)[0]

			switch policyName {
			case "CMP0116":
				return false, nil
			default:
				return false, fmt.Errorf("policy %s not implemented", policyName)
			}
		default:
			return false, fmt.Errorf("if op not implemented: %s", op)
		}
	} else {
		return false, fmt.Errorf("unhandled if statement: len(args) == %d", len(args))
	}
}

// Eval implements CMakeStatement.
func (i *IfStatement) Eval(eval *CMakeEvaluatorScope) error {
	cond, err := i.evalCondition(eval)
	if err != nil {
		return err
	}

	if cond {
		return eval.eval.evalBlock(eval, i.Body)
	} else {
		return eval.eval.evalBlock(eval, i.Else)
	}
}

// cmakeStatement implements CMakeStatement.
func (i *IfStatement) cmakeStatement() { panic("unimplemented") }

func (i *IfStatement) String() string {
	return fmt.Sprintf("If then %+v else %+v", i.Body, i.Else)
}

type MacroStatement struct {
	ast.CommandInvocation

	Body []ast.CommandInvocation
}

// Eval implements CMakeStatement.
func (m *MacroStatement) Eval(eval *CMakeEvaluatorScope) error {
	args := m.Arguments.Eval(eval.evaluator)

	if len(args) == 1 {
		name := args[0]

		eval.eval.commands[name] = func(scope *CMakeEvaluatorScope, args []string) error {
			child := scope.childScope("")

			child.Set("ARGV", strings.Join(args, " "))

			stmts, err := eval.eval.parseStatement(m.Body)
			if err != nil {
				return err
			}

			return child.eval.evalBlock(child, stmts)
		}

		return nil
	} else {
		return fmt.Errorf("macro not implemented: len(args) = %d", len(args))
	}
}

// cmakeStatement implements CMakeStatement.
func (m *MacroStatement) cmakeStatement() { panic("unimplemented") }

var (
	_ CMakeStatement = &CommandStatement{}
	_ CMakeStatement = &IfStatement{}
	_ CMakeStatement = &MacroStatement{}
)

type CMakeCommand func(scope *CMakeEvaluatorScope, args []string) error

type CMakeEvaluatorScope struct {
	eval *CMakeEvaluator

	evaluator *evaluator
}

// Get implements starlark.HasSetKey.
func (eval *CMakeEvaluatorScope) Get(k starlark.Value) (v starlark.Value, found bool, err error) {
	return eval.evaluator.values.Get(k)
}

// SetKey implements starlark.HasSetKey.
func (eval *CMakeEvaluatorScope) SetKey(k starlark.Value, v starlark.Value) error {
	return eval.evaluator.values.SetKey(k, v)
}

func (eval *CMakeEvaluatorScope) childScope(dirname string) *CMakeEvaluatorScope {
	child := &CMakeEvaluatorScope{
		eval: eval.eval,
		evaluator: &evaluator{
			parent: eval.evaluator,
			values: starlark.NewDict(32),
		},
	}

	if dirname != "" {
		child.Set("CMAKE_CURRENT_LIST_DIR", dirname)
	}

	return child
}

func (eval *CMakeEvaluatorScope) Set(k string, v string) {
	eval.evaluator.values.SetKey(starlark.String(k), starlark.String(v))
}

func (*CMakeEvaluatorScope) String() string { return "CMakeEvaluatorScope" }
func (*CMakeEvaluatorScope) Type() string   { return "CMakeEvaluatorScope" }
func (*CMakeEvaluatorScope) Hash() (uint32, error) {
	return 0, fmt.Errorf("CMakeEvaluatorScope is not hashable")
}
func (*CMakeEvaluatorScope) Truth() starlark.Bool { return starlark.True }
func (*CMakeEvaluatorScope) Freeze()              {}

var (
	_ starlark.Value     = &CMakeEvaluatorScope{}
	_ starlark.HasSetKey = &CMakeEvaluatorScope{}
)

type CMakeEvaluator struct {
	base       string
	sourceRoot *StarDirectory

	commands map[string]CMakeCommand
}

func (eval *CMakeEvaluator) parseIfStatement(rest []ast.CommandInvocation) (CMakeStatement, []ast.CommandInvocation, error) {
	if rest[0].Name != "if" {
		return nil, nil, fmt.Errorf("parseIfStatement: rest[0].Name != \"if\"")
	}

	ret := &IfStatement{
		CommandInvocation: rest[0],
	}

	inElse := false

	rest = rest[1:]

outer:
	for {
		for i, cmd := range rest {
			if cmd.Name == "endif" {
				// slog.Info("if", "body", ret.Body, "else", ret.Else)
				return ret, rest[i+1:], nil
			} else if cmd.Name == "else" {
				inElse = true
			} else if cmd.Name == "if" {
				stmt, newRest, err := eval.parseIfStatement(rest[i:])
				if err != nil {
					return nil, nil, err
				}

				if inElse {
					ret.Else = append(ret.Else, stmt)
				} else {
					ret.Body = append(ret.Body, stmt)
				}

				rest = newRest

				continue outer
			} else if cmd.Name == "macro" {
				stmt, newRest, err := eval.parseMacroStatement(rest[i:])
				if err != nil {
					return nil, nil, err
				}

				if inElse {
					ret.Else = append(ret.Else, stmt)
				} else {
					ret.Body = append(ret.Body, stmt)
				}

				rest = newRest

				continue outer
			} else {
				if inElse {
					ret.Else = append(ret.Else, &CommandStatement{CommandInvocation: cmd})
				} else {
					ret.Body = append(ret.Body, &CommandStatement{CommandInvocation: cmd})
				}
			}
		}
		break outer
	}

	return nil, nil, fmt.Errorf("could not find an endif before the file ended")
}

func (eval *CMakeEvaluator) parseMacroStatement(rest []ast.CommandInvocation) (CMakeStatement, []ast.CommandInvocation, error) {
	if rest[0].Name != "macro" {
		return nil, nil, fmt.Errorf("parseMacroStatement: rest[0].Name != \"macro\"")
	}

	ret := &MacroStatement{CommandInvocation: rest[0]}

	rest = rest[1:]

	for i, cmd := range rest {
		if cmd.Name == "endmacro" {
			return ret, rest[i+1:], nil
		} else {
			ret.Body = append(ret.Body, cmd)
		}
	}

	return nil, nil, fmt.Errorf("could not find an endmacro before the file ended")
}

func (eval *CMakeEvaluator) parseStatement(cmds []ast.CommandInvocation) ([]CMakeStatement, error) {
	var ret []CMakeStatement

	for i, cmd := range cmds {
		if cmd.Name == "if" {
			stmt, newRest, err := eval.parseIfStatement(cmds[i:])
			if err != nil {
				return nil, err
			}
			ret = append(ret, stmt)

			rest, err := eval.parseStatement(newRest)
			if err != nil {
				return nil, err
			}

			return append(ret, rest...), nil
		} else if cmd.Name == "macro" {
			stmt, newRest, err := eval.parseMacroStatement(cmds[i:])
			if err != nil {
				return nil, err
			}
			ret = append(ret, stmt)

			rest, err := eval.parseStatement(newRest)
			if err != nil {
				return nil, err
			}

			return append(ret, rest...), nil
		} else {
			ret = append(ret, &CommandStatement{CommandInvocation: cmd})
		}
	}

	return ret, nil
}

func (eval *CMakeEvaluator) evalBlock(scope *CMakeEvaluatorScope, stmts []CMakeStatement) error {
	// slog.Info("evalBlock", "stmts", stmts)

	for _, stmt := range stmts {
		if err := stmt.Eval(scope); err != nil {
			return err
		}
	}

	return nil
}

func (eval *CMakeEvaluator) evalFile(scope *CMakeEvaluatorScope, filename string, ast *ast.CMakeFile) error {
	child := scope.childScope(path.Dir(filename))

	stmts, err := eval.parseStatement(ast.Commands)
	if err != nil {
		return err
	}

	if err := eval.evalBlock(child, stmts); err != nil {
		return err
	}

	return nil
}

func (eval *CMakeEvaluator) evalCommand(scope *CMakeEvaluatorScope, name string, args []string) error {
	cmd, ok := eval.commands[name]
	if !ok {
		return fmt.Errorf("command %s not found", name)
	}

	return cmd(scope, args)
}

func (eval *CMakeEvaluator) open(path string) (StarFileIf, bool, error) {
	path = strings.TrimPrefix(path, eval.base)

	slog.Info("open", "path", path)

	return eval.sourceRoot.openChild(path, false)
}

func (eval *CMakeEvaluator) searchForFile(name string, paths []string) (StarFileIf, error) {
	slog.Info("searchForFile", "name", name, "paths", paths)
	child, ok, err := eval.open(name)
	if err != nil {
		return nil, err
	}
	if ok {
		return child, nil
	}

	for _, path := range paths {
		dir, ok, err := eval.open(path)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}

		if dir, ok := dir.(*StarDirectory); ok {
			for childName, child := range dir.entries {
				if childName == name {
					return child, nil
				}
			}
		} else {
			return nil, fmt.Errorf("item in search path is not a directory: %s", path)
		}
	}

	return nil, fmt.Errorf("file not found in paths: %s", name)
}

func (eval *CMakeEvaluator) evalInclude(scope *CMakeEvaluatorScope, name string, opts []string) error {
	slog.Info("include", "name", name, "opts", opts)

	searchPath := strings.Split(scope.evaluator.Get("CMAKE_MODULE_PATH"), " ")

	if !strings.HasSuffix(name, ".cmake") {
		name = name + ".cmake"
	}

	file, err := eval.searchForFile(name, searchPath)
	if err != nil {
		return err
	}

	if err := eval.evalFileIf(scope, file); err != nil {
		return err
	}

	return nil
}

func (eval *CMakeEvaluator) evalFileIf(scope *CMakeEvaluatorScope, f StarFileIf) error {
	fh, err := f.Open()
	if err != nil {
		return err
	}
	defer fh.Close()

	parser := ast.NewParser()

	ast, err := parser.Parse(fh)
	if err != nil {
		return err
	}

	if err := eval.evalFile(scope, f.Name(), ast); err != nil {
		return err
	}

	return nil
}

func evalCMake(f *StarDirectory, ctx *starlark.Dict, cmds *starlark.Dict) (starlark.Value, error) {
	top, ok, err := f.openChild("CMakeLists.txt", false)
	if err != nil {
		return starlark.None, err
	}
	if !ok {
		return starlark.None, fmt.Errorf("could not find CMakeLists.txt at the top of the archive")
	}

	fh, err := top.Open()
	if err != nil {
		return starlark.None, err
	}
	defer fh.Close()

	parser := ast.NewParser()

	ast, err := parser.Parse(fh)
	if err != nil {
		return starlark.None, err
	}

	eval := &CMakeEvaluator{
		base:       f.Name() + "/",
		sourceRoot: f,
		commands:   make(map[string]CMakeCommand),
	}

	eval.commands["include"] = func(scope *CMakeEvaluatorScope, args []string) error {
		return eval.evalInclude(scope, args[0], args[1:])
	}

	cmds.Entries(func(k, v starlark.Value) bool {
		kStr, ok := starlark.AsString(k)
		if !ok {
			slog.Warn("could not convert to string", "type", k.Type())
		}

		eval.commands[kStr] = func(scope *CMakeEvaluatorScope, args []string) error {
			var argValues starlark.Tuple

			for _, arg := range args {
				argValues = append(argValues, starlark.String(arg))
			}

			thread := &starlark.Thread{}

			_, err := starlark.Call(thread, v, starlark.Tuple{scope, argValues}, []starlark.Tuple{})
			if err != nil {
				return err
			}

			return nil
		}

		return true
	})

	scope := &CMakeEvaluatorScope{
		eval: eval,
		evaluator: &evaluator{
			values: ctx,
		},
	}

	if err := eval.evalFile(scope, top.Name(), ast); err != nil {
		return starlark.None, err
	}

	return starlark.None, fmt.Errorf("not implemented")
}

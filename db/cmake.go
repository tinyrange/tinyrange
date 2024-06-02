package db

import (
	"fmt"
	"log/slog"

	"github.com/kythe/llvmbzlgen/cmakelib/ast"
	"go.starlark.net/starlark"
)

type CMakeStatement interface {
	cmakeStatement()

	Eval(eval *CMakeEvaluator) error
}

type CommandStatement struct {
	ast.CommandInvocation
}

// Eval implements CMakeStatement.
func (c *CommandStatement) Eval(eval *CMakeEvaluator) error {
	args := c.Arguments.Eval(eval.evaluator)

	return eval.evalCommand(c.Name, args)
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

// Eval implements CMakeStatement.
func (i *IfStatement) Eval(eval *CMakeEvaluator) error {
	args := i.Arguments.Eval(eval.evaluator)

	not := false

	if args[0] == "NOT" {
		not = true
		args = args[1:]
	}

	slog.Info("if", "not", not, "args", args)

	compare := false

	if args[1] == "STREQUAL" {
		compare = args[0] == args[2]
	} else {
		slog.Info("if not implemented", "args", args)

		return fmt.Errorf("if not implemented")
	}

	if not {
		compare = !compare
	}

	if compare {
		slog.Info("eval then", "block", i.Body)
		return eval.evalBlock(i.Body)
	} else {
		slog.Info("eval else", "block", i.Else)
		return eval.evalBlock(i.Else)
	}
}

// cmakeStatement implements CMakeStatement.
func (i *IfStatement) cmakeStatement() { panic("unimplemented") }

func (i *IfStatement) String() string {
	return fmt.Sprintf("If then %+v else %+v", i.Body, i.Else)
}

var (
	_ CMakeStatement = &CommandStatement{}
	_ CMakeStatement = &IfStatement{}
)

type CMakeCommand func(args []string) error

type evaluator struct {
	values *starlark.Dict
}

// Get implements ast.Bindings.
func (eval *evaluator) Get(k string) string {
	v, ok, err := eval.values.Get(starlark.String(k))
	if err != nil {
		slog.Error("CMakeEvaluator: failed to get", "k", k, "err", err)
	}

	if !ok {
		return ""
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
		return ""
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
		return ""
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

type CMakeEvaluator struct {
	commands map[string]CMakeCommand

	evaluator *evaluator
}

// Get implements starlark.HasSetKey.
func (eval *CMakeEvaluator) Get(k starlark.Value) (v starlark.Value, found bool, err error) {
	return eval.evaluator.values.Get(k)
}

// SetKey implements starlark.HasSetKey.
func (eval *CMakeEvaluator) SetKey(k starlark.Value, v starlark.Value) error {
	return eval.evaluator.values.SetKey(k, v)
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

	return nil, nil, fmt.Errorf("could not find a endif before the file ended")
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

			return append(append(ret, stmt), rest...), nil
		} else {
			ret = append(ret, &CommandStatement{CommandInvocation: cmd})
		}
	}

	return ret, nil
}

func (eval *CMakeEvaluator) evalBlock(stmts []CMakeStatement) error {
	for _, stmt := range stmts {
		if err := stmt.Eval(eval); err != nil {
			return err
		}
	}

	return nil
}

func (eval *CMakeEvaluator) evalFile(ast *ast.CMakeFile) error {
	stmts, err := eval.parseStatement(ast.Commands)
	if err != nil {
		return err
	}

	if err := eval.evalBlock(stmts); err != nil {
		return err
	}

	return nil
}

func (eval *CMakeEvaluator) evalCommand(name string, args []string) error {
	cmd, ok := eval.commands[name]
	if !ok {
		return fmt.Errorf("command %s not found", name)
	}

	return cmd(args)
}

func (*CMakeEvaluator) String() string        { return "CMakeEvaluator" }
func (*CMakeEvaluator) Type() string          { return "CMakeEvaluator" }
func (*CMakeEvaluator) Hash() (uint32, error) { return 0, fmt.Errorf("CMakeEvaluator is not hashable") }
func (*CMakeEvaluator) Truth() starlark.Bool  { return starlark.True }
func (*CMakeEvaluator) Freeze()               {}

var (
	_ starlark.Value     = &CMakeEvaluator{}
	_ starlark.HasSetKey = &CMakeEvaluator{}
)

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
		commands: make(map[string]CMakeCommand),
		evaluator: &evaluator{
			values: ctx,
		},
	}

	cmds.Entries(func(k, v starlark.Value) bool {
		kStr, ok := starlark.AsString(k)
		if !ok {
			slog.Warn("could not convert to string", "type", k.Type())
		}

		eval.commands[kStr] = func(args []string) error {
			var argValues starlark.Tuple

			for _, arg := range args {
				argValues = append(argValues, starlark.String(arg))
			}

			thread := &starlark.Thread{}

			_, err := starlark.Call(thread, v, starlark.Tuple{eval, argValues}, []starlark.Tuple{})
			if err != nil {
				return err
			}

			return nil
		}

		return true
	})

	if err := eval.evalFile(ast); err != nil {
		return starlark.None, err
	}

	return starlark.None, fmt.Errorf("not implemented")
}

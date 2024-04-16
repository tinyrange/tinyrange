package db

import (
	"bytes"
	"fmt"
	"strings"

	"go.starlark.net/starlark"
	"mvdan.cc/sh/v3/syntax"
)

type shellCommand struct {
	name string
	f    *starlark.Function
}

func (cmd *shellCommand) Run(ctx *ShellContext, argv []string) (string, error) {
	thread := &starlark.Thread{}

	var args starlark.Tuple

	for _, arg := range argv {
		args = append(args, starlark.String(arg))
	}

	ret, err := starlark.Call(thread, cmd.f, starlark.Tuple{ctx, args}, []starlark.Tuple{})
	if err != nil {
		return "", err
	}

	str, ok := starlark.AsString(ret)
	if !ok {
		return "", fmt.Errorf("value %T could not be converted to a string", ret)
	}

	return str, nil
}

type ShellContext struct {
	out      *starlark.Dict
	commands map[string]*shellCommand
}

func (p *ShellContext) runCommand(args []string) (string, error) {
	cmd, ok := p.commands[args[0]]
	if !ok {
		return "", fmt.Errorf("command not found: %s", args[0])
	}

	return cmd.Run(p, args)
}

func (p *ShellContext) getParam(name string) (string, error) {
	val, ok, err := p.out.Get(starlark.String(name))
	if err != nil {
		return "", err
	} else if !ok {
		return "", nil
	}

	str, _ := starlark.AsString(val)

	return str, nil
}

func (p *ShellContext) evaluatePart(part syntax.WordPart) (string, error) {
	switch part := part.(type) {
	case *syntax.Lit:
		return part.Value, nil
	case *syntax.ParamExp:
		param, err := p.getParam(part.Param.Value)
		if err != nil {
			return "", err
		}
		return param, nil
	case *syntax.SglQuoted:
		if part.Dollar {
			return "", fmt.Errorf("single quotes with dollar not implemented")
		} else {
			return part.Value, nil
		}
	case *syntax.DblQuoted:
		var ret []string

		// ret = append(ret, "\"")

		for _, part := range part.Parts {
			val, err := p.evaluatePart(part)
			if err != nil {
				return "", err
			}

			ret = append(ret, val)
		}

		// ret = append(ret, "\"")

		return strings.Join(ret, ""), nil
	case *syntax.CmdSubst:
		var ret []string

		for _, stmt := range part.Stmts {
			out, err := p.visitStmt(stmt, "")
			if err != nil {
				return "", err
			}

			ret = append(ret, out)
		}

		return strings.Join(ret, ""), nil
	default:
		return "", fmt.Errorf("word part %T not implemented", part)
	}
}

func (p *ShellContext) evaluateWord(word *syntax.Word) (string, error) {
	var ret []string

	for _, part := range word.Parts {
		val, err := p.evaluatePart(part)
		if err != nil {
			return "", err
		}

		ret = append(ret, val)
	}

	return strings.Join(ret, ""), nil
}

func (p *ShellContext) visitStmt(stmt *syntax.Stmt, stdin string) (string, error) {
	switch cmd := stmt.Cmd.(type) {
	case *syntax.CallExpr:
		if len(cmd.Assigns) == 0 {
			var args []string
			for _, arg := range cmd.Args {
				val, err := p.evaluateWord(arg)
				if err != nil {
					return "", err
				}
				args = append(args, val)
			}

			return p.runCommand(args)
		} else {
			for _, assign := range cmd.Assigns {
				k := assign.Name.Value

				if assign.Value != nil {
					val, err := p.evaluateWord(assign.Value)
					if err != nil {
						return "", err
					}

					if err := p.out.SetKey(starlark.String(k), starlark.String(val)); err != nil {
						return "", err
					}
				}
			}

			return "", nil
		}
	case *syntax.FuncDecl:
		return "", fmt.Errorf("FuncDecl not implemented")
	case *syntax.IfClause:
		// TODO(joshua): Implement ifclause.
		return "", nil
	case *syntax.DeclClause:
		switch cmd.Variant.Value {
		case "export":
			for _, assign := range cmd.Args {
				k := assign.Name.Value

				if assign.Value != nil {
					val, err := p.evaluateWord(assign.Value)
					if err != nil {
						return "", err
					}

					if err := p.out.SetKey(starlark.String(k), starlark.String(val)); err != nil {
						return "", err
					}
				}
			}

			return "", nil
		default:
			return "", fmt.Errorf("DeclClause: %s not implemented", cmd.Variant.Value)
		}
	case *syntax.BinaryCmd:
		switch cmd.Op {
		case syntax.Pipe:
			lhs, err := p.visitStmt(cmd.X, "")
			if err != nil {
				return "", err
			}

			rhs, err := p.visitStmt(cmd.Y, lhs)
			if err != nil {
				return "", err
			}

			return rhs, nil
		default:
			return "", fmt.Errorf("BinaryCmd op %s not implemented", cmd.Op.String())
		}
	default:
		return "", fmt.Errorf("statement %T not implemented", cmd)
	}
}

func (t *ShellContext) String() string { return "GitTree" }
func (*ShellContext) Type() string     { return "GitTree" }
func (*ShellContext) Hash() (uint32, error) {
	return 0, fmt.Errorf("GitTree is not hashable")
}
func (*ShellContext) Truth() starlark.Bool { return starlark.True }
func (*ShellContext) Freeze()              {}

// Attr implements starlark.HasAttrs.
func (p *ShellContext) Attr(name string) (starlark.Value, error) {
	if name == "eval" {
		return starlark.NewBuiltin("Shell.eval", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				contents string
			)

			if err := starlark.UnpackArgs("Shell.eval", args, kwargs,
				"contents", &contents,
			); err != nil {
				return starlark.None, err
			}

			parser := syntax.NewParser()

			f, err := parser.Parse(bytes.NewReader([]byte(contents)), "")
			if err != nil {
				return nil, err
			}

			for _, stmt := range f.Stmts {
				if _, err := p.visitStmt(stmt, ""); err != nil {
					return nil, err
				}
			}

			return p.out, nil
		}), nil
	} else if name == "add_command" {
		return starlark.NewBuiltin("Shell.eval", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				name string
				f    *starlark.Function
			)

			if err := starlark.UnpackArgs("Shell.add_command", args, kwargs,
				"name", &name,
				"f", &f,
			); err != nil {
				return starlark.None, err
			}

			p.commands[name] = &shellCommand{name: name, f: f}

			return starlark.None, nil
		}), nil
	} else if name == "set_environment" {
		return starlark.NewBuiltin("Shell.set_environment", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				key   string
				value string
			)

			if err := starlark.UnpackArgs("Shell.eval", args, kwargs,
				"key", &key,
				"value", &value,
			); err != nil {
				return starlark.None, err
			}

			if err := p.out.SetKey(starlark.String(key), starlark.String(value)); err != nil {
				return starlark.None, err
			}

			return starlark.None, nil
		}), nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (p *ShellContext) AttrNames() []string {
	return []string{"eval", "add_command", "set_environment"}
}

var (
	_ starlark.Value    = &ShellContext{}
	_ starlark.HasAttrs = &ShellContext{}
)

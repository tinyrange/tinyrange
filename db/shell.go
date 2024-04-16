package db

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"

	"go.starlark.net/starlark"
	"mvdan.cc/sh/v3/syntax"
)

type shellParser struct {
	out *starlark.Dict
}

func (p *shellParser) getParam(name string) (string, error) {
	val, ok, err := p.out.Get(starlark.String(name))
	if err != nil {
		return "", err
	} else if !ok {
		return "", nil
	}

	str, _ := starlark.AsString(val)

	return str, nil
}

func (p *shellParser) evaluatePart(part syntax.WordPart) (string, error) {
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

func (p *shellParser) evaluateWord(word *syntax.Word) (string, error) {
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

func (p *shellParser) visitStmt(stmt *syntax.Stmt, stdin string) (string, error) {
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

			slog.Info("call", "args", args)

			return "", nil
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

func parseShell(content string) (starlark.Value, error) {
	p := syntax.NewParser()

	f, err := p.Parse(bytes.NewReader([]byte(content)), "")
	if err != nil {
		return nil, err
	}

	dict := starlark.NewDict(32)

	parser := &shellParser{out: dict}

	for _, stmt := range f.Stmts {
		if _, err := parser.visitStmt(stmt, ""); err != nil {
			return nil, err
		}
	}

	return dict, nil
}

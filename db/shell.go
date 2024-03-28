package db

import (
	"bytes"
	"fmt"
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

func (p *shellParser) visitStmt(stmt *syntax.Stmt) error {
	switch cmd := stmt.Cmd.(type) {
	case *syntax.CallExpr:
		for _, assign := range cmd.Assigns {
			k := assign.Name.Value

			val, err := p.evaluateWord(assign.Value)
			if err != nil {
				return err
			}

			if err := p.out.SetKey(starlark.String(k), starlark.String(val)); err != nil {
				return err
			}
		}
	case *syntax.FuncDecl:
	case *syntax.IfClause:
	default:
		return fmt.Errorf("statement %T not implemented", cmd)
	}

	return nil
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
		if err := parser.visitStmt(stmt); err != nil {
			return nil, err
		}
	}

	return dict, nil
}

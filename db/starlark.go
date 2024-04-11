package db

import (
	"fmt"

	"github.com/go-python/gpython/ast"
	"github.com/go-python/gpython/parser"
	"github.com/go-python/gpython/py"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

func evalStarlark(contents string, kwArgs []starlark.Tuple) (starlark.Value, error) {
	thread := &starlark.Thread{}

	env := starlark.StringDict{}

	for _, arg := range kwArgs {
		k, _ := starlark.AsString(arg[0])
		v := arg[1]

		env[k] = v
	}

	declared, err := starlark.ExecFileOptions(&syntax.FileOptions{
		TopLevelControl: true,
		Recursion:       true,
		Set:             true,
		GlobalReassign:  true,
	}, thread, "<eval>", contents, env)
	if err != nil {
		return starlark.None, err
	}

	ret := starlark.NewDict(len(declared))
	for k, v := range declared {
		ret.SetKey(starlark.String(k), v)
	}

	return ret, err
}

func emitStarlark(node ast.Ast) (syntax.Node, error) {
	switch node := node.(type) {
	case *ast.Module:
		ret := &syntax.File{Options: &syntax.FileOptions{
			TopLevelControl: true,
			Recursion:       true,
			Set:             true,
			GlobalReassign:  true,
		}}

		for _, child := range node.Body {
			node, err := emitStarlark(child)
			if err != nil {
				return nil, err
			}

			stmt, ok := node.(syntax.Stmt)
			if !ok {
				return nil, fmt.Errorf("emitStarlark for %T made a %T but expected syntax.Stmt", child, node)
			}

			ret.Stmts = append(ret.Stmts, stmt)
		}

		return ret, nil
	case *ast.Assign:
		var (
			lhs syntax.Node
			err error
		)
		if len(node.Targets) == 1 {
			lhs, err = emitStarlark(node.Targets[0])
			if err != nil {
				return nil, err
			}
		} else {
			return nil, fmt.Errorf("assignments to multiple values not implemented")
		}

		lhsExpr, ok := lhs.(syntax.Expr)
		if !ok {
			return nil, fmt.Errorf("emitStarlark for %T made a %T but expected syntax.Expr", node.Targets[0], lhs)
		}

		rhs, err := emitStarlark(node.Value)
		if err != nil {
			return nil, err
		}

		rhsExpr, ok := rhs.(syntax.Expr)
		if !ok {
			return nil, fmt.Errorf("emitStarlark for %T made a %T but expected syntax.Expr", node.Value, rhs)
		}

		return &syntax.AssignStmt{Op: syntax.EQ, LHS: lhsExpr, RHS: rhsExpr}, nil
	case *ast.AugAssign:
		var op syntax.Token

		switch node.Op {
		case ast.Add:
			op = syntax.PLUS_EQ
		default:
			return nil, fmt.Errorf("unknown augmented operation: %+v", node.Op)
		}

		lhs, err := emitStarlark(node.Target)
		if err != nil {
			return nil, err
		}

		lhsExpr, ok := lhs.(syntax.Expr)
		if !ok {
			return nil, fmt.Errorf("emitStarlark for %T made a %T but expected syntax.Expr", node.Target, lhs)
		}

		rhs, err := emitStarlark(node.Value)
		if err != nil {
			return nil, err
		}

		rhsExpr, ok := rhs.(syntax.Expr)
		if !ok {
			return nil, fmt.Errorf("emitStarlark for %T made a %T but expected syntax.Expr", node.Value, rhs)
		}

		return &syntax.AssignStmt{Op: op, LHS: lhsExpr, RHS: rhsExpr}, nil
	case *ast.Dict:
		ret := &syntax.DictExpr{}

		if len(node.Keys) != len(node.Values) {
			return nil, fmt.Errorf("len(node.Keys) != len(node.Values)")
		}

		for i := 0; i < len(node.Keys); i++ {
			k := node.Keys[i]
			v := node.Values[i]

			kNode, err := emitStarlark(k)
			if err != nil {
				return nil, err
			}

			kExpr, ok := kNode.(syntax.Expr)
			if !ok {
				return nil, fmt.Errorf("emitStarlark for %T made a %T but expected syntax.Expr", k, kNode)
			}

			vNode, err := emitStarlark(v)
			if err != nil {
				return nil, err
			}

			vExpr, ok := vNode.(syntax.Expr)
			if !ok {
				return nil, fmt.Errorf("emitStarlark for %T made a %T but expected syntax.Expr", v, vNode)
			}

			ret.List = append(ret.List, &syntax.DictEntry{
				Key:   kExpr,
				Value: vExpr,
			})
		}

		return ret, nil
	case *ast.List:
		ret := &syntax.ListExpr{}

		for _, element := range node.Elts {
			node, err := emitStarlark(element)
			if err != nil {
				return nil, err
			}

			expr, ok := node.(syntax.Expr)
			if !ok {
				return nil, fmt.Errorf("emitStarlark for %T made a %T but expected syntax.Expr", element, node)
			}

			ret.List = append(ret.List, expr)
		}

		return ret, nil
	case *ast.Tuple:
		ret := &syntax.TupleExpr{}

		for _, element := range node.Elts {
			node, err := emitStarlark(element)
			if err != nil {
				return nil, err
			}

			expr, ok := node.(syntax.Expr)
			if !ok {
				return nil, fmt.Errorf("emitStarlark for %T made a %T but expected syntax.Expr", element, node)
			}

			ret.List = append(ret.List, expr)
		}

		return ret, nil
	case *ast.BinOp:
		var op syntax.Token

		switch node.Op {
		case ast.Modulo:
			op = syntax.PERCENT
		case ast.Add:
			op = syntax.PLUS
		default:
			return nil, fmt.Errorf("unknown operation: %+v", node.Op)
		}

		lhs, err := emitStarlark(node.Left)
		if err != nil {
			return nil, err
		}

		lhsExpr, ok := lhs.(syntax.Expr)
		if !ok {
			return nil, fmt.Errorf("emitStarlark for %T made a %T but expected syntax.Expr", node.Left, lhs)
		}

		rhs, err := emitStarlark(node.Right)
		if err != nil {
			return nil, err
		}

		rhsExpr, ok := rhs.(syntax.Expr)
		if !ok {
			return nil, fmt.Errorf("emitStarlark for %T made a %T but expected syntax.Expr", node.Right, rhs)
		}

		return &syntax.BinaryExpr{
			X:  lhsExpr,
			Op: op,
			Y:  rhsExpr,
		}, nil
	case *ast.UnaryOp:
		var op syntax.Token

		switch node.Op {
		case ast.Not:
			op = syntax.NOT
		case ast.USub:
			op = syntax.MINUS
		default:
			return nil, fmt.Errorf("unknown operation: %+v", node.Op)
		}

		lhs, err := emitStarlark(node.Operand)
		if err != nil {
			return nil, err
		}

		lhsExpr, ok := lhs.(syntax.Expr)
		if !ok {
			return nil, fmt.Errorf("emitStarlark for %T made a %T but expected syntax.Expr", node.Operand, lhs)
		}

		return &syntax.UnaryExpr{
			X:  lhsExpr,
			Op: op,
		}, nil
	case *ast.Subscript:
		value, err := emitStarlark(node.Value)
		if err != nil {
			return nil, err
		}

		valueExpr, ok := value.(syntax.Expr)
		if !ok {
			return nil, fmt.Errorf("emitStarlark for %T made a %T but expected syntax.Expr", node.Value, value)
		}

		ret := &syntax.SliceExpr{X: valueExpr}

		switch slicer := node.Slice.(type) {
		case *ast.Slice:
			if slicer.Lower != nil {
				lowerValue, err := emitStarlark(slicer.Lower)
				if err != nil {
					return nil, err
				}

				lowerExpr, ok := lowerValue.(syntax.Expr)
				if !ok {
					return nil, fmt.Errorf("emitStarlark for %T made a %T but expected syntax.Expr", slicer.Lower, lowerValue)
				}

				ret.Lo = lowerExpr
			}

			if slicer.Upper != nil {
				upperValue, err := emitStarlark(slicer.Upper)
				if err != nil {
					return nil, err
				}

				upperExpr, ok := upperValue.(syntax.Expr)
				if !ok {
					return nil, fmt.Errorf("emitStarlark for %T made a %T but expected syntax.Expr", slicer.Upper, upperValue)
				}

				ret.Hi = upperExpr
			}

			if slicer.Step != nil {
				stepValue, err := emitStarlark(slicer.Step)
				if err != nil {
					return nil, err
				}

				stepExpr, ok := stepValue.(syntax.Expr)
				if !ok {
					return nil, fmt.Errorf("emitStarlark for %T made a %T but expected syntax.Expr", slicer.Step, stepValue)
				}

				ret.Step = stepExpr
			}

			return ret, nil
		case *ast.Index:
			indexValue, err := emitStarlark(slicer.Value)
			if err != nil {
				return nil, err
			}

			indexExpr, ok := indexValue.(syntax.Expr)
			if !ok {
				return nil, fmt.Errorf("emitStarlark for %T made a %T but expected syntax.Expr", slicer.Value, indexValue)
			}

			return &syntax.IndexExpr{
				X: valueExpr,
				Y: indexExpr,
			}, nil
		default:
			return nil, fmt.Errorf("unknown slicer %T: %+v", slicer, slicer)
		}
	case *ast.ListComp:
		ret := &syntax.Comprehension{Curly: false}

		value, err := emitStarlark(node.Elt)
		if err != nil {
			return nil, err
		}

		valueExpr, ok := value.(syntax.Expr)
		if !ok {
			return nil, fmt.Errorf("emitStarlark for %T made a %T but expected syntax.Expr", node.Elt, value)
		}

		ret.Body = valueExpr

		for _, gen := range node.Generators {
			targetValue, err := emitStarlark(gen.Target)
			if err != nil {
				return nil, err
			}

			targetExpr, ok := targetValue.(syntax.Expr)
			if !ok {
				return nil, fmt.Errorf("emitStarlark for %T made a %T but expected syntax.Expr", gen.Target, targetValue)
			}

			iterValue, err := emitStarlark(gen.Iter)
			if err != nil {
				return nil, err
			}

			iterExpr, ok := iterValue.(syntax.Expr)
			if !ok {
				return nil, fmt.Errorf("emitStarlark for %T made a %T but expected syntax.Expr", gen.Iter, iterValue)
			}

			ret.Clauses = append(ret.Clauses, &syntax.ForClause{
				Vars: targetExpr,
				X:    iterExpr,
			})

			for _, test := range gen.Ifs {
				testValue, err := emitStarlark(test)
				if err != nil {
					return nil, err
				}

				testExpr, ok := iterValue.(syntax.Expr)
				if !ok {
					return nil, fmt.Errorf("emitStarlark for %T made a %T but expected syntax.Expr", test, testValue)
				}

				ret.Clauses = append(ret.Clauses, &syntax.IfClause{
					Cond: testExpr,
				})
			}
		}

		return ret, nil
	case *ast.GeneratorExp:
		ret := &syntax.Comprehension{Curly: false}

		value, err := emitStarlark(node.Elt)
		if err != nil {
			return nil, err
		}

		valueExpr, ok := value.(syntax.Expr)
		if !ok {
			return nil, fmt.Errorf("emitStarlark for %T made a %T but expected syntax.Expr", node.Elt, value)
		}

		ret.Body = valueExpr

		if len(node.Generators) != 1 {
			return nil, fmt.Errorf("len(node.Generators) != 1")
		}

		gen := node.Generators[0]

		targetValue, err := emitStarlark(gen.Target)
		if err != nil {
			return nil, err
		}

		targetExpr, ok := targetValue.(syntax.Expr)
		if !ok {
			return nil, fmt.Errorf("emitStarlark for %T made a %T but expected syntax.Expr", gen.Target, targetValue)
		}

		iterValue, err := emitStarlark(gen.Iter)
		if err != nil {
			return nil, err
		}

		iterExpr, ok := iterValue.(syntax.Expr)
		if !ok {
			return nil, fmt.Errorf("emitStarlark for %T made a %T but expected syntax.Expr", gen.Iter, iterValue)
		}

		ret.Clauses = append(ret.Clauses, &syntax.ForClause{
			Vars: targetExpr,
			X:    iterExpr,
		})

		for _, test := range gen.Ifs {
			testValue, err := emitStarlark(test)
			if err != nil {
				return nil, err
			}

			testExpr, ok := iterValue.(syntax.Expr)
			if !ok {
				return nil, fmt.Errorf("emitStarlark for %T made a %T but expected syntax.Expr", test, testValue)
			}

			ret.Clauses = append(ret.Clauses, &syntax.IfClause{
				Cond: testExpr,
			})
		}

		return ret, nil
	case *ast.Call:
		ret := &syntax.CallExpr{}

		value, err := emitStarlark(node.Func)
		if err != nil {
			return nil, err
		}

		valueExpr, ok := value.(syntax.Expr)
		if !ok {
			return nil, fmt.Errorf("emitStarlark for %T made a %T but expected syntax.Expr", node.Func, value)
		}

		ret.Fn = valueExpr

		for _, arg := range node.Args {
			argValue, err := emitStarlark(arg)
			if err != nil {
				return nil, err
			}

			argExpr, ok := argValue.(syntax.Expr)
			if !ok {
				return nil, fmt.Errorf("emitStarlark for %T made a %T but expected syntax.Expr", arg, argValue)
			}

			ret.Args = append(ret.Args, argExpr)
		}

		for _, kw := range node.Keywords {
			value, err := emitStarlark(kw.Value)
			if err != nil {
				return nil, err
			}

			valueExpr, ok := value.(syntax.Expr)
			if !ok {
				return nil, fmt.Errorf("emitStarlark for %T made a %T but expected syntax.Expr", kw.Value, value)
			}

			ret.Args = append(ret.Args, &syntax.BinaryExpr{
				Op: syntax.EQ,
				X:  &syntax.Ident{Name: string(kw.Arg)},
				Y:  valueExpr,
			})
		}

		if node.Starargs != nil {
			return nil, fmt.Errorf("node.Starargs != nil")
		}

		if node.Kwargs != nil {
			return nil, fmt.Errorf("node.Kwargs != nil")
		}

		return ret, nil
	case *ast.Compare:
		var op syntax.Token

		if len(node.Ops) != 1 {
			return nil, fmt.Errorf("len(node.Ops) != 1")
		}
		if len(node.Comparators) != 1 {
			return nil, fmt.Errorf("len(node.Comparators) != 1")
		}

		nodeOp := node.Ops[0]
		nodeRight := node.Comparators[0]

		switch nodeOp {
		case ast.NotEq:
			op = syntax.NEQ
		default:
			return nil, fmt.Errorf("unknown operation: %+v", nodeOp)
		}

		lhs, err := emitStarlark(node.Left)
		if err != nil {
			return nil, err
		}

		lhsExpr, ok := lhs.(syntax.Expr)
		if !ok {
			return nil, fmt.Errorf("emitStarlark for %T made a %T but expected syntax.Expr", node.Left, lhs)
		}

		rhs, err := emitStarlark(nodeRight)
		if err != nil {
			return nil, err
		}

		rhsExpr, ok := rhs.(syntax.Expr)
		if !ok {
			return nil, fmt.Errorf("emitStarlark for %T made a %T but expected syntax.Expr", nodeRight, rhs)
		}

		return &syntax.BinaryExpr{
			X:  lhsExpr,
			Op: op,
			Y:  rhsExpr,
		}, nil
	case *ast.Attribute:
		value, err := emitStarlark(node.Value)
		if err != nil {
			return nil, err
		}

		valueExpr, ok := value.(syntax.Expr)
		if !ok {
			return nil, fmt.Errorf("emitStarlark for %T made a %T but expected syntax.Expr", node.Value, value)
		}

		return &syntax.DotExpr{
			X:    valueExpr,
			Name: &syntax.Ident{Name: string(node.Attr)},
		}, nil
	case *ast.Name:
		return &syntax.Ident{Name: string(node.Id)}, nil
	case *ast.Str:
		return &syntax.Literal{Token: syntax.STRING, Value: string(node.S)}, nil
	case *ast.Num:
		switch obj := node.N.(type) {
		case py.Int:
			return &syntax.Literal{Token: syntax.INT, Value: int64(obj)}, nil
		default:
			return nil, fmt.Errorf("unknown object %T: %+v", obj, obj)
		}
	case *ast.NameConstant:
		if node.Value == py.True {
			return &syntax.Ident{Name: "True"}, nil
		} else if node.Value == py.False {
			return &syntax.Ident{Name: "False"}, nil
		} else {
			return nil, fmt.Errorf("unknown singleton %T: %+v", node.Value, node.Value)
		}
	default:
		return nil, fmt.Errorf("node not implemented %T: %+v", node, node)
	}
}

func evalPython(contents string, kwArgs []starlark.Tuple) (starlark.Value, error) {
	ast, err := parser.ParseString(contents, py.ExecMode)
	if err != nil {
		return starlark.None, fmt.Errorf("error parsing: %s", err)
	}

	translated, err := emitStarlark(ast)
	if err != nil {
		return starlark.None, err
	}

	env := starlark.StringDict{}

	for _, arg := range kwArgs {
		k, _ := starlark.AsString(arg[0])
		v := arg[1]

		env[k] = v
	}

	prog, err := starlark.FileProgram(translated.(*syntax.File), env.Has)
	if err != nil {
		return starlark.None, err
	}

	thread := &starlark.Thread{}

	declared, err := prog.Init(thread, env)
	if err != nil {
		return starlark.None, err
	}

	ret := starlark.NewDict(len(declared))
	for k, v := range declared {
		ret.SetKey(starlark.String(k), v)
	}

	return ret, err
}

package utils

import "github.com/tinyrange/tinyrange/cmd/structgen/sysil2/emitgo"

func Cast(target emitgo.Node, source emitgo.Node) emitgo.Node {
	return emitgo.CastExpression{
		Target: target,
		Source: source,
	}
}

func Call(target emitgo.Node, args ...emitgo.Node) emitgo.Node {
	return emitgo.CallExpression{
		Target:    target,
		Arguments: args,
	}
}

func Array(typ emitgo.Node, elements ...int64) emitgo.Node {
	return emitgo.ArrayType{
		Type:     typ,
		Elements: elements[0],
	}
}

func Comment(s string) emitgo.Node {
	return emitgo.Comment(s)
}

func Tuple(elements ...emitgo.Node) emitgo.Node {
	return emitgo.Tuple(elements)
}

func List(elements ...emitgo.Node) emitgo.Node {
	return emitgo.List(elements)
}

func Return(value ...emitgo.Node) emitgo.Node {
	return emitgo.ReturnExpression{Value: emitgo.List(value)}
}

func Var(name emitgo.Identifier, typ emitgo.Node, value ...emitgo.Node) emitgo.Node {
	if len(value) == 0 {
		return emitgo.VariableDeclaration{Name: name, Type: typ}
	} else {
		return emitgo.VariableDeclaration{Name: name, Type: typ, Value: value[0]}
	}
}

func Assign(target emitgo.Node, expr emitgo.Node) emitgo.Node {
	return &emitgo.AssignExpression{Target: target, Expr: expr}
}

func If(expr emitgo.Node, body ...emitgo.Node) emitgo.Node {
	return &emitgo.IfStatement{Expr: expr, Body: emitgo.Block(body)}
}

func ForMax(binding emitgo.Identifier, count int64, body ...emitgo.Node) emitgo.Node {
	return emitgo.ForLoopStatement{
		Initial:     emitgo.RawStringf("%s := 0", binding),
		Termination: emitgo.RawStringf("%s < %d", binding, count),
		Step:        emitgo.RawStringf("%s++", binding),
		Body:        emitgo.Block(body),
	}
}

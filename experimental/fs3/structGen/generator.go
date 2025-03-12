package main

import (
	"fmt"
	"go/ast"
	"go/format"
	goToken "go/token"
	"io"
	"log/slog"
	"strconv"
	"strings"
)

type typeInfo struct {
	goType ast.Expr
	size   int
}

var builtinToGoType = map[typeInstanceBuiltin]*typeInfo{
	typeInstanceBuiltinUint8:    {ast.NewIdent("uint8"), 1},
	typeInstanceBuiltinUint64LE: {ast.NewIdent("uint64"), 8},
	typeInstanceBuiltinUint64BE: {ast.NewIdent("uint64"), 8},

	typeInstanceBuiltinInt8:    {ast.NewIdent("int8"), 1},
	typeInstanceBuiltinInt16LE: {ast.NewIdent("int16"), 2},
	typeInstanceBuiltinInt16BE: {ast.NewIdent("int16"), 2},
	typeInstanceBuiltinInt32LE: {ast.NewIdent("int32"), 4},
	typeInstanceBuiltinInt32BE: {ast.NewIdent("int32"), 4},

	typeInstanceBuiltinChar: {ast.NewIdent("byte"), 1},
}

func asGoName(s string) string {
	// convert snake_case to CamelCase
	// e.g. "foo_bar" -> "FooBar"
	var ret string

	upper := true
	for _, r := range s {
		if r == '_' {
			upper = true
			continue
		}

		if upper {
			ret += string(r)
			upper = false
		} else {
			ret += string(r)
		}
	}

	return ret
}

func exportName(s string) string {
	n := asGoName(s)

	// make the first character uppercase
	return strings.ToUpper(n[:1]) + n[1:]
}

func addExpr(a, b ast.Expr) ast.Expr {
	if a == nil {
		return b
	}

	return &ast.BinaryExpr{
		X:  a,
		Op: goToken.ADD,
		Y:  b,
	}
}

type sysIL4Generator struct {
	packageName string

	declarations  []ast.Decl
	declaredTypes map[string]*typeInfo
}

func (g *sysIL4Generator) declareType(name string, typ *typeInfo) {
	g.declarations = append(g.declarations, &ast.GenDecl{
		Tok:   goToken.TYPE,
		Specs: []ast.Spec{&ast.TypeSpec{Name: ast.NewIdent(name), Type: typ.goType}},
	})

	g.declaredTypes[name] = typ
}

func (g *sysIL4Generator) declareConstant(name string, typ ast.Expr, value ast.Expr) {
	g.declarations = append(g.declarations, &ast.GenDecl{
		Tok: goToken.CONST,
		Specs: []ast.Spec{&ast.ValueSpec{
			Names:  []*ast.Ident{ast.NewIdent(name)},
			Type:   typ,
			Values: []ast.Expr{value},
		}},
	})
}

func (g *sysIL4Generator) evaluateSizeExpression(expr expression) (int, error) {
	switch expr := expr.(type) {
	case numberLiteralExpression:
		val, err := strconv.ParseInt(string(expr), 0, 64)
		if err != nil {
			return 0, err
		}

		return int(val), nil
	default:
		return 0, fmt.Errorf("unexpected expression: %T", expr)
	}
}

func (g *sysIL4Generator) typeExprFromInstance(inst typeInstance) (*typeInfo, error) {
	switch inst := inst.(type) {
	case typeInstanceBuiltin:
		typ, ok := builtinToGoType[inst]
		if !ok {
			return nil, fmt.Errorf("unsupported builtin type: %v", inst)
		}

		return typ, nil
	case typeInstanceReference:
		dec, ok := g.declaredTypes[string(inst)]
		if !ok {
			return nil, fmt.Errorf("undeclared type: %s", inst)
		}

		return dec, nil
	case *typeInstanceArray:
		elementType, err := g.typeExprFromInstance(inst.elementType)
		if err != nil {
			return nil, err
		}

		length, err := g.generateExpression(inst.size)
		if err != nil {
			return nil, err
		}

		lengthInt, err := g.evaluateSizeExpression(inst.size)
		if err != nil {
			return nil, err
		}

		return &typeInfo{
			goType: &ast.ArrayType{
				Elt: elementType.goType,
				Len: length,
			},
			size: elementType.size * lengthInt,
		}, nil
	default:
		return nil, fmt.Errorf("unexpected type instance: %T", inst)
	}
}

func (g *sysIL4Generator) generateExpression(expr expression) (ast.Expr, error) {
	switch expr := expr.(type) {
	case numberLiteralExpression:
		return &ast.BasicLit{Kind: goToken.INT, Value: string(expr)}, nil
	case variableReferenceExpression:
		return ast.NewIdent(string(expr)), nil
	case *binaryExpression:
		lhs, err := g.generateExpression(expr.lhs)
		if err != nil {
			return nil, err
		}

		rhs, err := g.generateExpression(expr.rhs)
		if err != nil {
			return nil, err
		}

		var op goToken.Token
		switch expr.op {
		case binaryOpMinus:
			op = goToken.SUB
		default:
			return nil, fmt.Errorf("unexpected binary operator: %v", expr.op)
		}

		return &ast.BinaryExpr{
			X:  lhs,
			Op: op,
			Y:  rhs,
		}, nil
	default:
		return nil, fmt.Errorf("unexpected expression: %T", expr)
	}
}

func (g *sysIL4Generator) generateEnumDeclaration(name string, decl *enumDeclaration) error {
	typ, err := g.typeExprFromInstance(decl.valueType)
	if err != nil {
		return err
	}

	enumName := exportName(name)
	enumTyp := ast.NewIdent(enumName)

	g.declareType(enumName, typ)

	for _, member := range decl.members {
		value, err := g.generateExpression(member.value)
		if err != nil {
			return err
		}

		g.declareConstant(enumName+exportName(member.name), enumTyp, value)
	}

	// TODO(joshua): Generate a String method for the enum type.

	return nil
}

func (g *sysIL4Generator) generateStructDeclaration(name string, decl *structDeclaration) error {
	structType := ast.NewIdent(exportName(name))

	_ = structType

	var currentLength ast.Expr

	for _, member := range decl.members {
		switch member := member.(type) {
		case *structOrUnionMember:
			typ, err := g.typeExprFromInstance(member.valueType)
			if err != nil {
				return err
			}

			// Generate a Get and Set method for the member.

		case *structConstMember:
			value, err := g.generateExpression(member.value)
			if err != nil {
				return err
			}

			g.declareConstant(exportName(name)+exportName(member.name), nil, value)
		default:
			return fmt.Errorf("unexpected member type: %T", member)
		}
	}

	if decl.dynamic {
		// use a []byte for the dynamic struct
		g.declareType(exportName(name), &ast.ArrayType{
			Elt: ast.NewIdent("byte"),
		})
	} else {
		// use a fixed length byte array for the static struct
		g.declareType(exportName(name), &ast.ArrayType{
			Elt: ast.NewIdent("byte"),
			Len: currentLength,
		})
	}

	return nil
}

func (g *sysIL4Generator) generateBitsetDeclaration(name string, decl *bitsetDeclaration) error {
	slog.Info("generateBitsetDeclaration", "name", exportName(name))

	return nil
}

func (g *sysIL4Generator) generateUnionDeclaration(name string, decl *unionDeclaration) error {
	slog.Info("generateUnionDeclaration", "name", exportName(name))

	return nil
}

func (g *sysIL4Generator) generateTypeDeclaration(decl *typeDeclaration) error {
	switch inner := decl.innerType.(type) {
	case *enumDeclaration:
		return g.generateEnumDeclaration(decl.name, inner)
	case *structDeclaration:
		return g.generateStructDeclaration(decl.name, inner)
	case *bitsetDeclaration:
		return g.generateBitsetDeclaration(decl.name, inner)
	case *unionDeclaration:
		return g.generateUnionDeclaration(decl.name, inner)
	default:
		return fmt.Errorf("unexpected inner type: %T", inner)
	}
}

func (g *sysIL4Generator) generate(ast *file) error {
	for _, decl := range ast.declarations {
		switch decl := decl.(type) {
		case *typeDeclaration:
			if err := g.generateTypeDeclaration(decl); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unexpected declaration type: %T", decl)
		}
	}

	return nil
}

func (g *sysIL4Generator) writeTo(w io.Writer) error {
	if _, err := fmt.Fprintf(w, "// generated by structGen. DO NOT EDIT MANUALLY\n\n"); err != nil {
		return err
	}

	return format.Node(w, goToken.NewFileSet(), &ast.File{
		Name:  ast.NewIdent(g.packageName),
		Decls: g.declarations,
	})
}

func newSysIL4Generator(packageName string) *sysIL4Generator {
	ret := &sysIL4Generator{
		packageName:   packageName,
		declaredTypes: make(map[string]*typeInfo),
	}

	return ret
}

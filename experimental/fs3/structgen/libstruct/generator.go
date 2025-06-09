package libstruct

import (
	"fmt"
	"go/ast"
	"go/format"
	goToken "go/token"
	"io"
	"strconv"
	"strings"
)

type TypeInfo interface {
	GoType() ast.Expr
	Size() int
	IsDynamic() bool
	CastFromBytes(expr ast.Expr) ast.Expr
	StoreToBytes(expr ast.Expr, value ast.Expr) ast.Stmt
}

type simpleTypeInfo struct {
	goType ast.Expr
	size   int
}

func (t *simpleTypeInfo) GoType() ast.Expr {
	return t.goType
}

func (t *simpleTypeInfo) String() string {
	return fmt.Sprintf("{goType: %v, size: %d}", t.goType, t.size)
}

func (t *simpleTypeInfo) Size() int {
	return t.size
}

func (t *simpleTypeInfo) IsDynamic() bool {
	return t.size < 0
}

func (t *simpleTypeInfo) CastFromBytes(expr ast.Expr) ast.Expr {
	if t.size == 1 {
		// special case for uint8
		return castExpr(t.goType, &ast.IndexExpr{X: expr, Index: ast.NewIdent("0")})
	}

	return castExpr(t.goType, expr)
}

func (t *simpleTypeInfo) StoreToBytes(expr ast.Expr, value ast.Expr) ast.Stmt {
	if t.size == 1 {
		// special case for uint8
		return &ast.AssignStmt{
			Lhs: []ast.Expr{&ast.IndexExpr{X: expr, Index: ast.NewIdent("0")}},
			Tok: goToken.ASSIGN,
			Rhs: []ast.Expr{castExpr(ast.NewIdent("byte"), value)},
		}
	}

	return &ast.ExprStmt{
		X: &ast.CallExpr{
			Fun:  ast.NewIdent("copy"),
			Args: []ast.Expr{expr, &ast.SliceExpr{X: value}},
		},
	}
}

var (
	_ TypeInfo = &simpleTypeInfo{}
)

type builtinTypeInfo struct {
	goType    ast.Expr
	size      int
	bigEndian bool
	unsigned  bool
}

func (t *builtinTypeInfo) GoType() ast.Expr {
	return t.goType
}

func (t *builtinTypeInfo) String() string {
	return fmt.Sprintf("{goType: %v, size: %d, bigEndian: %v, unsigned: %v}", t.goType, t.size, t.bigEndian, t.unsigned)
}

func (t *builtinTypeInfo) Size() int {
	return t.size
}

func (t *builtinTypeInfo) IsDynamic() bool {
	return false
}

func (t *builtinTypeInfo) CastFromBytes(expr ast.Expr) ast.Expr {
	if t.size == 1 {
		// special case for uint8
		return castExpr(t.goType, &ast.IndexExpr{X: expr, Index: ast.NewIdent("0")})
	}

	var endian ast.Expr
	if t.bigEndian {
		endian = &ast.SelectorExpr{X: ast.NewIdent("binary"), Sel: ast.NewIdent("BigEndian")}
	} else {
		endian = &ast.SelectorExpr{X: ast.NewIdent("binary"), Sel: ast.NewIdent("LittleEndian")}
	}

	call := &ast.CallExpr{
		Fun: &ast.SelectorExpr{X: endian, Sel: ast.NewIdent("Uint" + strconv.Itoa(t.size*8))},
		Args: []ast.Expr{
			expr,
		},
	}

	if t.unsigned {
		return call
	} else {
		return castExpr(t.goType, call)
	}
}

func (t *builtinTypeInfo) StoreToBytes(expr ast.Expr, value ast.Expr) ast.Stmt {
	if t.size == 1 {
		// special case for uint8
		return &ast.AssignStmt{
			Lhs: []ast.Expr{&ast.IndexExpr{X: expr, Index: ast.NewIdent("0")}},
			Tok: goToken.ASSIGN,
			Rhs: []ast.Expr{castExpr(ast.NewIdent("byte"), value)},
		}
	}

	var endian ast.Expr
	if t.bigEndian {
		endian = &ast.SelectorExpr{X: ast.NewIdent("binary"), Sel: ast.NewIdent("BigEndian")}
	} else {
		endian = &ast.SelectorExpr{X: ast.NewIdent("binary"), Sel: ast.NewIdent("LittleEndian")}
	}

	if !t.unsigned {
		// figure out the name of the unsigned version of the type
		signedType := "uint" + strconv.Itoa(t.size*8)

		// cast the value to the signed version of the type
		value = castExpr(ast.NewIdent(signedType), value)
	}

	return &ast.ExprStmt{
		X: &ast.CallExpr{
			Fun: &ast.SelectorExpr{X: endian, Sel: ast.NewIdent("PutUint" + strconv.Itoa(t.size*8))},
			Args: []ast.Expr{
				expr,
				value,
			},
		},
	}
}

var (
	_ TypeInfo = &builtinTypeInfo{}
)

func newBuiltinTypeInfo(goType string, size int, bigEndian, unsigned bool) *builtinTypeInfo {
	return &builtinTypeInfo{
		goType:    ast.NewIdent(goType),
		size:      size,
		bigEndian: bigEndian,
		unsigned:  unsigned,
	}
}

var builtinToGoType = map[TypeInstanceBuiltin]TypeInfo{
	TypeInstanceBuiltinUint8:    newBuiltinTypeInfo("uint8", 1, false, true),
	TypeInstanceBuiltinUint16LE: newBuiltinTypeInfo("uint16", 2, false, true),
	TypeInstanceBuiltinUint16BE: newBuiltinTypeInfo("uint16", 2, true, true),
	TypeInstanceBuiltinUint32LE: newBuiltinTypeInfo("uint32", 4, false, true),
	TypeInstanceBuiltinUint32BE: newBuiltinTypeInfo("uint32", 4, true, true),
	TypeInstanceBuiltinUint64LE: newBuiltinTypeInfo("uint64", 8, false, true),
	TypeInstanceBuiltinUint64BE: newBuiltinTypeInfo("uint64", 8, true, true),
	TypeInstanceBuiltinInt8:     newBuiltinTypeInfo("int8", 1, false, false),
	TypeInstanceBuiltinInt16LE:  newBuiltinTypeInfo("int16", 2, false, false),
	TypeInstanceBuiltinInt16BE:  newBuiltinTypeInfo("int16", 2, true, false),
	TypeInstanceBuiltinInt32LE:  newBuiltinTypeInfo("int32", 4, false, false),
	TypeInstanceBuiltinInt32BE:  newBuiltinTypeInfo("int32", 4, true, false),
	TypeInstanceBuiltinInt64LE:  newBuiltinTypeInfo("int64", 8, false, false),
	TypeInstanceBuiltinInt64BE:  newBuiltinTypeInfo("int64", 8, true, false),
	TypeInstanceBuiltinChar:     newBuiltinTypeInfo("byte", 1, false, true),
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
			ret += strings.ToUpper(string(r))
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

func intValue(value int) ast.Expr {
	return &ast.BasicLit{Kind: goToken.INT, Value: strconv.Itoa(value)}
}

func castExpr(typ ast.Expr, expr ast.Expr) ast.Expr {
	return &ast.CallExpr{
		Fun:  typ,
		Args: []ast.Expr{expr},
	}
}

func sliceExpr(expr ast.Expr, start, end ast.Expr) ast.Expr {
	return &ast.SliceExpr{
		X:    expr,
		Low:  start,
		High: end,
	}
}

func pointerTo(typ ast.Expr) ast.Expr {
	return &ast.StarExpr{
		X: typ,
	}
}

func dotExpr(expr ast.Expr, sel string) ast.Expr {
	return &ast.SelectorExpr{
		X:   expr,
		Sel: ast.NewIdent(sel),
	}
}

func newString(value string) ast.Expr {
	return &ast.BasicLit{Kind: goToken.STRING, Value: strconv.Quote(value)}
}

var byteArray = &ast.ArrayType{
	Elt: ast.NewIdent("byte"),
}

type SysIL4Generator struct {
	packageName string

	declarations  []ast.Decl
	declaredTypes map[string]TypeInfo
}

func (g *SysIL4Generator) declareType(name string, underlyingType ast.Expr, typ TypeInfo) {
	// log.Default().Info("declareType", "name", name, "type", typ)

	g.declarations = append(g.declarations, &ast.GenDecl{
		Tok:   goToken.TYPE,
		Specs: []ast.Spec{&ast.TypeSpec{Name: ast.NewIdent(name), Type: underlyingType}},
	})

	g.declaredTypes[name] = typ
}

func (g *SysIL4Generator) declareConstant(name string, typ ast.Expr, value ast.Expr) {
	g.declarations = append(g.declarations, &ast.GenDecl{
		Tok: goToken.CONST,
		Specs: []ast.Spec{&ast.ValueSpec{
			Names:  []*ast.Ident{ast.NewIdent(name)},
			Type:   typ,
			Values: []ast.Expr{value},
		}},
	})
}

func (g *SysIL4Generator) declareMethod(receiver ast.Expr, bindingName *ast.Ident, name string, params []*ast.Field, results []*ast.Field, body []ast.Stmt) {
	g.declarations = append(g.declarations, &ast.FuncDecl{
		Recv: &ast.FieldList{
			List: []*ast.Field{
				{
					Names: []*ast.Ident{bindingName},
					Type:  receiver,
				},
			},
		},
		Name: ast.NewIdent(name),
		Type: &ast.FuncType{
			Params:  &ast.FieldList{List: params},
			Results: &ast.FieldList{List: results},
		},
		Body: &ast.BlockStmt{List: body},
	})
}

func (g *SysIL4Generator) evaluateSizeExpression(expr Expression) (int, error) {
	switch expr := expr.(type) {
	case NumberLiteralExpression:
		val, err := strconv.ParseInt(string(expr), 0, 64)
		if err != nil {
			return 0, err
		}

		return int(val), nil
	case VariableReferenceExpression:
		return -1, nil
	case *BinaryExpression:
		lhs, err := g.evaluateSizeExpression(expr.Lhs)
		if err != nil {
			return 0, err
		}

		rhs, err := g.evaluateSizeExpression(expr.Rhs)
		if err != nil {
			return 0, err
		}

		if lhs < 0 || rhs < 0 {
			return -1, nil
		}

		switch expr.Op {
		case BinaryOpPlus:
			return lhs + rhs, nil
		case BinaryOpMinus:
			return lhs - rhs, nil
		default:
			return 0, fmt.Errorf("unexpected binary operator: %v", expr.Op)
		}
	default:
		return 0, fmt.Errorf("unexpected expression for evaluateSizeExpression: %T", expr)
	}
}

func (g *SysIL4Generator) evaluateExpression(expr ast.Expr) (int, error) {
	if expr == nil {
		return 0, nil
	}

	switch expr := expr.(type) {
	case *ast.BasicLit:
		switch expr.Kind {
		case goToken.INT:
			val, err := strconv.ParseInt(expr.Value, 0, 64)
			if err != nil {
				return 0, err
			}

			return int(val), nil
		default:
			return 0, fmt.Errorf("unexpected basic literal kind: %v", expr.Kind)
		}
	case *ast.BinaryExpr:
		lhs, err := g.evaluateExpression(expr.X)
		if err != nil {
			return 0, err
		}

		rhs, err := g.evaluateExpression(expr.Y)
		if err != nil {
			return 0, err
		}

		switch expr.Op {
		case goToken.ADD:
			return lhs + rhs, nil
		case goToken.SUB:
			return lhs - rhs, nil
		default:
			return 0, fmt.Errorf("unexpected binary operator: %v", expr.Op)
		}
	default:
		return 0, fmt.Errorf("unexpected expression for evaluateExpression: %T", expr)
	}
}

func (g *SysIL4Generator) typeExprFromInstance(inst TypeInstance, currentSize ast.Expr) (TypeInfo, error) {
	switch inst := inst.(type) {
	case TypeInstanceBuiltin:
		typ, ok := builtinToGoType[inst]
		if !ok {
			return nil, fmt.Errorf("unsupported builtin type: %v", inst)
		}

		return typ, nil
	case TypeInstanceReference:
		exported := exportName(string(inst))

		dec, ok := g.declaredTypes[exported]
		if !ok {
			return nil, fmt.Errorf("undeclared type: %s", inst)
		}

		return dec, nil
	case *TypeInstanceArray:
		elementType, err := g.typeExprFromInstance(inst.ElementType, currentSize)
		if err != nil {
			return nil, err
		}

		length, err := g.generateExpression(inst.Size, currentSize)
		if err != nil {
			return nil, err
		}

		lengthInt, err := g.evaluateExpression(length)
		if err != nil {
			return nil, err
		}

		if lengthInt < 0 {
			return &simpleTypeInfo{
				goType: &ast.ArrayType{
					Elt: elementType.GoType(),
				},
				size: -1,
			}, nil
		} else {
			return &simpleTypeInfo{
				goType: &ast.ArrayType{
					Elt: elementType.GoType(),
					Len: length,
				},
				size: elementType.Size() * lengthInt,
			}, nil
		}
	default:
		return nil, fmt.Errorf("unexpected type instance: %T", inst)
	}
}

func (g *SysIL4Generator) generateExpression(expr Expression, currentSize ast.Expr) (ast.Expr, error) {
	switch expr := expr.(type) {
	case NumberLiteralExpression:
		return &ast.BasicLit{Kind: goToken.INT, Value: string(expr)}, nil
	case VariableReferenceExpression:
		if expr == "_" {
			if currentSize == nil {
				return nil, fmt.Errorf("size expression required")
			}

			return currentSize, nil
		}

		return ast.NewIdent(string(expr)), nil
	case *BinaryExpression:
		lhs, err := g.generateExpression(expr.Lhs, currentSize)
		if err != nil {
			return nil, err
		}

		rhs, err := g.generateExpression(expr.Rhs, currentSize)
		if err != nil {
			return nil, err
		}

		var op goToken.Token
		switch expr.Op {
		case BinaryOpPlus:
			op = goToken.ADD
		case BinaryOpMinus:
			op = goToken.SUB
		default:
			return nil, fmt.Errorf("unexpected binary operator: %v", expr.Op)
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

func (g *SysIL4Generator) generateEnumDeclaration(name string, decl *EnumDeclaration) error {
	typ, err := g.typeExprFromInstance(decl.ValueType, nil)
	if err != nil {
		return err
	}

	enumName := exportName(name)
	enumTyp := ast.NewIdent(enumName)

	g.declareType(enumName, typ.GoType(), &simpleTypeInfo{
		goType: enumTyp,
		size:   typ.Size(),
	})

	for _, member := range decl.Members {
		value, err := g.generateExpression(member.Value, nil)
		if err != nil {
			return err
		}

		g.declareConstant(enumName+exportName(member.Name), enumTyp, value)
	}

	// generate a String method as a series of switch cases
	var cases []ast.Stmt
	for _, member := range decl.Members {
		cases = append(cases, &ast.CaseClause{
			List: []ast.Expr{ast.NewIdent(enumName + exportName(member.Name))},
			Body: []ast.Stmt{
				&ast.ReturnStmt{
					Results: []ast.Expr{newString(member.Name)},
				},
			},
		})
	}

	g.declareMethod(enumTyp, ast.NewIdent("e"), "String", []*ast.Field{}, []*ast.Field{
		{
			Type: ast.NewIdent("string"),
		},
	}, []ast.Stmt{
		&ast.SwitchStmt{
			Tag: ast.NewIdent("e"),
			Body: &ast.BlockStmt{
				List: append(cases, &ast.CaseClause{
					Body: []ast.Stmt{
						&ast.ReturnStmt{
							Results: []ast.Expr{
								&ast.CallExpr{
									Fun: dotExpr(ast.NewIdent("fmt"), "Sprintf"),
									Args: []ast.Expr{
										newString("unknown <%d>"),
										ast.NewIdent("e"),
									},
								},
							},
						},
					},
				}),
			},
		},
	})

	return nil
}

func (g *SysIL4Generator) generateStructDeclaration(name string, decl *StructDeclaration) error {
	structType := ast.NewIdent(exportName(name))

	isDynamic := decl.Dynamic

	_ = structType

	var currentLength ast.Expr

	binding := ast.NewIdent("r")

	var expectedSize int64 = -1

	for _, member := range decl.Members {
		switch member := member.(type) {
		case *StructOrUnionMember:
			typ, err := g.typeExprFromInstance(member.ValueType, currentLength)
			if err != nil {
				return err
			}

			if typ.IsDynamic() && !isDynamic {
				return fmt.Errorf("dynamic member (%s) in static struct (%s)", member.Name, name)
			}

			if member.Name != "_" {
				startInt, err := g.evaluateExpression(currentLength)
				if err != nil {
					return err
				}

				endInt := startInt + typ.Size()

				// TODO(joshua): Generate a slice method that returns the raw binary slice.
				sliceMethod := ast.NewIdent(asGoName(member.Name) + "Slice")

				sliceRet := &ast.ArrayType{
					Elt: ast.NewIdent("byte"),
					Len: intValue(typ.Size()),
				}

				if typ.IsDynamic() {
					g.declareMethod(structType, binding, sliceMethod.Name, []*ast.Field{}, []*ast.Field{
						{
							Type: byteArray,
						},
					}, []ast.Stmt{
						&ast.ReturnStmt{
							Results: []ast.Expr{
								sliceExpr(binding, intValue(startInt), nil),
							},
						},
					})
				} else {
					g.declareMethod(structType, binding, sliceMethod.Name, []*ast.Field{}, []*ast.Field{
						{
							Type: sliceRet,
						},
					}, []ast.Stmt{
						&ast.ReturnStmt{
							Results: []ast.Expr{
								castExpr(sliceRet, sliceExpr(binding, intValue(startInt), intValue(endInt))),
							},
						},
					})
				}

				byteSlice := sliceExpr(binding, intValue(startInt), intValue(endInt))

				if typ.IsDynamic() {
					byteSlice = sliceExpr(binding, intValue(startInt), nil)
				}

				// Get Method
				g.declareMethod(structType, binding, "Get"+asGoName(member.Name), []*ast.Field{}, []*ast.Field{
					{
						Type: typ.GoType(),
					},
				}, []ast.Stmt{
					&ast.ReturnStmt{
						Results: []ast.Expr{
							typ.CastFromBytes(byteSlice),
						},
					},
				})
				// Set Method
				var setReceiver ast.Expr = pointerTo(structType)
				if isDynamic {
					setReceiver = structType
				}
				g.declareMethod(setReceiver, binding, "Set"+asGoName(member.Name), []*ast.Field{
					{
						Names: []*ast.Ident{ast.NewIdent("value")},
						Type:  typ.GoType(),
					},
				}, []*ast.Field{}, []ast.Stmt{
					typ.StoreToBytes(byteSlice, ast.NewIdent("value")),
				})
			}

			currentLength = addExpr(currentLength, intValue(typ.Size()))
		case *StructConstMember:
			value, err := g.generateExpression(member.Value, currentLength)
			if err != nil {
				return err
			}

			g.declareConstant(
				exportName(name)+exportName(member.Name),
				nil,
				value,
			)
		case *StructSizeMember:
			if expectedSize != -1 {
				return fmt.Errorf("multiple size members in struct")
			}

			size, err := g.evaluateSizeExpression(member.Size)
			if err != nil {
				return err
			}

			expectedSize = int64(size)
		default:
			return fmt.Errorf("unexpected member type: %T", member)
		}
	}

	if decl.Dynamic {
		// use a []byte for the dynamic struct
		g.declareType(exportName(name), byteArray, &simpleTypeInfo{
			goType: structType,
			size:   -1,
		})
	} else {
		var evaluatedSize int = 0

		if expectedSize != -1 {
			currentLength = &ast.BasicLit{Kind: goToken.INT, Value: strconv.FormatInt(expectedSize, 10)}
			evaluatedSize = int(expectedSize)
		} else if currentLength == nil {
			currentLength = &ast.BasicLit{Kind: goToken.INT, Value: "0"}
		} else {
			var err error
			evaluatedSize, err = g.evaluateExpression(currentLength)
			if err != nil {
				return err
			}
		}

		// Generate a Size method for the struct.
		g.declareMethod(structType, binding, "Size", []*ast.Field{}, []*ast.Field{
			{
				Type: ast.NewIdent("int64"),
			},
		}, []ast.Stmt{
			&ast.ReturnStmt{
				Results: []ast.Expr{currentLength},
			},
		})

		// Generate a ReadAt method for the struct.
		// func (t BlockGroupDescriptor) ReadAt(p []byte, off int64) (int, error) {
		// 	if off > t.Size() {
		// 		return 0, io.EOF
		// 	}
		// 	return copy(p, t[off:]), nil
		// }
		g.declareMethod(structType, binding, "ReadAt", []*ast.Field{
			{
				Names: []*ast.Ident{ast.NewIdent("buf")},
				Type:  byteArray,
			},
			{
				Names: []*ast.Ident{ast.NewIdent("off")},
				Type:  ast.NewIdent("int64"),
			},
		}, []*ast.Field{
			{
				Type: ast.NewIdent("int"),
			},
			{
				Type: ast.NewIdent("error"),
			},
		}, []ast.Stmt{
			&ast.IfStmt{
				Cond: &ast.BinaryExpr{
					X:  ast.NewIdent("off"),
					Op: goToken.GTR,
					Y: &ast.CallExpr{
						Fun:  dotExpr(binding, "Size"),
						Args: []ast.Expr{},
					},
				},
				Body: &ast.BlockStmt{
					List: []ast.Stmt{
						&ast.ReturnStmt{
							Results: []ast.Expr{
								intValue(0),
								dotExpr(ast.NewIdent("io"), "EOF"),
							},
						},
					},
				},
			},
			&ast.ReturnStmt{
				Results: []ast.Expr{
					&ast.CallExpr{
						Fun:  ast.NewIdent("copy"),
						Args: []ast.Expr{ast.NewIdent("buf"), sliceExpr(binding, ast.NewIdent("off"), nil)},
					},
					ast.NewIdent("nil"),
				},
			},
		})

		// Generate a WriteAt method for the struct.
		// func (t *BlockGroupDescriptor) WriteAt(p []byte, off int64) (int, error) {
		// 	if off > t.Size() {
		// 		return 0, io.EOF
		// 	}
		// 	return copy(t[off:], p), nil
		// }
		g.declareMethod(pointerTo(structType), binding, "WriteAt", []*ast.Field{
			{
				Names: []*ast.Ident{ast.NewIdent("buf")},
				Type:  byteArray,
			},
			{
				Names: []*ast.Ident{ast.NewIdent("off")},
				Type:  ast.NewIdent("int64"),
			},
		}, []*ast.Field{
			{
				Type: ast.NewIdent("int"),
			},
			{
				Type: ast.NewIdent("error"),
			},
		}, []ast.Stmt{
			&ast.IfStmt{
				Cond: &ast.BinaryExpr{
					X:  ast.NewIdent("off"),
					Op: goToken.GTR,
					Y: &ast.CallExpr{
						Fun:  dotExpr(binding, "Size"),
						Args: []ast.Expr{},
					},
				},
				Body: &ast.BlockStmt{
					List: []ast.Stmt{
						&ast.ReturnStmt{
							Results: []ast.Expr{
								intValue(0),
								dotExpr(ast.NewIdent("io"), "EOF"),
							},
						},
					},
				},
			},
			&ast.ReturnStmt{
				Results: []ast.Expr{
					&ast.CallExpr{
						Fun:  ast.NewIdent("copy"),
						Args: []ast.Expr{sliceExpr(binding, ast.NewIdent("off"), nil), ast.NewIdent("buf")},
					},
					ast.NewIdent("nil"),
				},
			},
		})

		// use a fixed length byte array for the static struct
		g.declareType(exportName(name), &ast.ArrayType{
			Elt: ast.NewIdent("byte"),
			Len: currentLength,
		}, &simpleTypeInfo{
			goType: structType,
			size:   evaluatedSize,
		})
	}

	// TODO(joshua): Generate ReadAt and WriteAt methods.

	return nil
}

func (g *SysIL4Generator) generateBitsetDeclaration(name string, decl *BitsetDeclaration) error {
	valueType, err := g.typeExprFromInstance(decl.ValueType, nil)
	if err != nil {
		return err
	}

	if valueType.IsDynamic() {
		return fmt.Errorf("dynamic bitset value type is not supported")
	}

	valueBits := valueType.Size() * 8

	bitsetType := ast.NewIdent(exportName(name))

	var currentValue = 1

	g.declareType(exportName(name), valueType.GoType(), &simpleTypeInfo{
		goType: bitsetType,
		size:   valueType.Size(),
	})

	for i, member := range decl.Members {
		if i >= valueBits {
			return fmt.Errorf("too many bitset members")
		}

		if member != "_" {
			g.declareConstant(exportName(name)+exportName(member), bitsetType, intValue(currentValue))
		}
		currentValue <<= 1
	}

	// Generate a Has method for the bitset.
	g.declareMethod(bitsetType, ast.NewIdent("b"), "Has", []*ast.Field{
		{
			Names: []*ast.Ident{ast.NewIdent("mask")},
			Type:  bitsetType,
		},
	}, []*ast.Field{
		{
			Type: ast.NewIdent("bool"),
		},
	}, []ast.Stmt{
		&ast.ReturnStmt{
			Results: []ast.Expr{
				&ast.BinaryExpr{
					X: &ast.BinaryExpr{
						X:  ast.NewIdent("b"),
						Op: goToken.AND,
						Y:  ast.NewIdent("mask"),
					},
					Op: goToken.NEQ,
					Y:  intValue(0),
				},
			},
		},
	})

	return nil
}

func (g *SysIL4Generator) generateUnionDeclaration(name string, decl *UnionDeclaration) error {
	unionType := ast.NewIdent(exportName(name))

	var maxSize int

	binding := ast.NewIdent("u")

	for _, member := range decl.Members {
		switch member := member.(type) {
		case *StructOrUnionMember:
			typ, err := g.typeExprFromInstance(member.ValueType, nil)
			if err != nil {
				return err
			}

			if typ.IsDynamic() {
				return fmt.Errorf("dynamic union member type is not supported")
			}

			if typ.Size() > maxSize {
				maxSize = typ.Size()
			}

			// Generate a Get and Set method for the member.
			g.declareMethod(unionType, binding, "Get"+asGoName(member.Name), []*ast.Field{}, []*ast.Field{
				{
					Type: typ.GoType(),
				},
			}, []ast.Stmt{
				&ast.ReturnStmt{
					Results: []ast.Expr{
						typ.CastFromBytes(sliceExpr(binding, nil, nil)),
					},
				},
			})

			g.declareMethod(unionType, binding, "Set"+asGoName(member.Name), []*ast.Field{
				{
					Names: []*ast.Ident{ast.NewIdent("value")},
					Type:  typ.GoType(),
				},
			}, []*ast.Field{}, []ast.Stmt{
				typ.StoreToBytes(sliceExpr(binding, nil, nil), ast.NewIdent("value")),
			})
		default:
			return fmt.Errorf("unexpected member type: %T", member)
		}
	}

	// use a byte array with the maximum size for the union
	g.declareType(exportName(name), &ast.ArrayType{
		Elt: ast.NewIdent("byte"),
		Len: intValue(maxSize),
	}, &simpleTypeInfo{
		goType: unionType,
		size:   maxSize,
	})

	return nil
}

func (g *SysIL4Generator) generateTypeDeclaration(decl *TypeDeclaration) error {
	switch inner := decl.InnerType.(type) {
	case *EnumDeclaration:
		return g.generateEnumDeclaration(decl.Name, inner)
	case *StructDeclaration:
		return g.generateStructDeclaration(decl.Name, inner)
	case *BitsetDeclaration:
		return g.generateBitsetDeclaration(decl.Name, inner)
	case *UnionDeclaration:
		return g.generateUnionDeclaration(decl.Name, inner)
	default:
		return fmt.Errorf("unexpected inner type: %T", inner)
	}
}

func (g *SysIL4Generator) Generate(ast *File) error {
	for _, decl := range ast.Declarations {
		switch decl := decl.(type) {
		case *TypeDeclaration:
			if err := g.generateTypeDeclaration(decl); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unexpected declaration type: %T", decl)
		}
	}

	return nil
}

func (g *SysIL4Generator) WriteTo(w io.Writer) error {
	if _, err := fmt.Fprintf(w, "// generated by structGen. DO NOT EDIT MANUALLY\n\n"); err != nil {
		return err
	}

	// add import of "encoding/binary"
	g.declarations = append([]ast.Decl{&ast.GenDecl{
		Tok: goToken.IMPORT,
		Specs: []ast.Spec{
			&ast.ImportSpec{
				Path: &ast.BasicLit{Kind: goToken.STRING, Value: "\"encoding/binary\""},
			},
			&ast.ImportSpec{
				Path: &ast.BasicLit{Kind: goToken.STRING, Value: "\"io\""},
			},
			&ast.ImportSpec{
				Path: &ast.BasicLit{Kind: goToken.STRING, Value: "\"fmt\""},
			},
		},
	}}, g.declarations...)

	return format.Node(w, goToken.NewFileSet(), &ast.File{
		Name:  ast.NewIdent(g.packageName),
		Decls: g.declarations,
	})
}

func NewSysIL4Generator(packageName string) *SysIL4Generator {
	ret := &SysIL4Generator{
		packageName:   packageName,
		declaredTypes: make(map[string]TypeInfo),
	}

	return ret
}

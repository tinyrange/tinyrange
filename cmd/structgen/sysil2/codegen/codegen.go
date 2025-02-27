package codegen

import (
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/tinyrange/tinyrange/cmd/structgen/sysil2/emitgo"
	"github.com/tinyrange/tinyrange/cmd/structgen/sysil2/parser"
)

func makeGoName(s string, public bool) emitgo.Identifier {
	s = ToLowerCamel(s)

	if public {
		first := s[0]
		return emitgo.Identifier(strings.ToUpper(string(first)) + s[1:])
	} else {
		return emitgo.Identifier(s)
	}
}

type Value interface {
	Type() string
}

type NumberValue int64

// Type implements Value.
func (NumberValue) Type() string { return "number" }

var (
	_ Value = NumberValue(0)
)

func toInt64(val Value) (int64, error) {
	switch val := val.(type) {
	case NumberValue:
		return int64(val), nil
	default:
		return 0, fmt.Errorf("toInt64 not implemented: %T %+v", val, val)
	}
}

type Type interface {
	Size() int64
}

type TopLevelType interface {
	Type

	SetName(name string) error
	GenerateCode() (emitgo.Block, error)
}

type BasicType struct {
	size    int64
	goType  emitgo.BasicType
	decoder func(slice emitgo.Node) emitgo.Node
	encoder func(target emitgo.Node, value emitgo.Node) emitgo.Node
}

// Size implements Type.
func (ty *BasicType) Size() int64 { return ty.size }

var (
	_ Type = &BasicType{}
)

type StructureMember struct {
	Name string
	Type Type
}

func (sm *StructureMember) String() string {
	return fmt.Sprintf("%s %s", sm.Name, sm.Type)
}

type StructureField struct {
	// TODO(joshua): Support more fields besides join.

	Name    string
	Type    Type
	LhsName string
	RhsName string
}

type StructureType struct {
	name string

	Parent Type

	Members []*StructureMember
	Fields  []*StructureField
}

func (st *StructureType) GoType() emitgo.Identifier {
	if st.name == "" {
		panic("name not set")
	}

	return makeGoName(st.name, true)
}

func (st *StructureType) Encoder(target emitgo.Node, value emitgo.Identifier) emitgo.Node {
	// 	copy(t[(15 + (i * 20)):(15 + ((i + 1) * 20))], val[:])
	return emitgo.CallExpression{
		Target: emitgo.RawStringf("copy"),
		Arguments: []emitgo.Node{
			target,
			emitgo.ArrayAccessRangeExpression{
				Target: value,
			},
		},
	}
}

func (st *StructureType) Decoder(slice emitgo.Node) emitgo.Node {
	return emitgo.CallExpression{Target: st.GoType(), Arguments: []emitgo.Node{slice}}
}

// SetName implements TopLevelType.
func (st *StructureType) SetName(name string) error {
	st.name = name

	return nil
}

// GenerateCode implements Type.
func (st *StructureType) GenerateCode() (emitgo.Block, error) {
	var currentOffset int64 = 0

	var ret emitgo.Block

	var fieldStrings []emitgo.Node

	fieldStrings = append(fieldStrings, emitgo.StringLiteral(st.name+"{"))

	goName := makeGoName(st.name, true)

	binding := emitgo.Identifier("t")

	memberTypes := make(map[string]*BasicType)

	for _, member := range st.Members {
		switch typ := member.Type.(type) {
		case *BasicType:
			name := makeGoName(member.Name, true)

			slice := emitgo.ArrayAccessRangeExpression{
				Target: binding,
				Start:  emitgo.NumberLiteral(fmt.Sprintf("%d", currentOffset)),
				End:    emitgo.NumberLiteral(fmt.Sprintf("%d", currentOffset+typ.size)),
			}

			// Generate a Getter
			ret = append(ret, emitgo.FunctionDeclaration{
				BindName:   binding,
				BindType:   goName,
				Name:       name,
				ReturnType: typ.goType,
				Body: emitgo.Block{
					emitgo.ReturnExpression{Value: typ.decoder(slice)},
				},
			})

			fieldStrings = append(fieldStrings, emitgo.CallExpression{
				Target: emitgo.RawStringf("fmt.Sprintf"),
				Arguments: []emitgo.Node{
					emitgo.StringLiteral(fmt.Sprintf("%s=%%v", member.Name)),
					emitgo.CallExpression{Target: emitgo.MemberExpression{Target: binding, Name: name}},
				},
			})

			// Generate a Setter
			ret = append(ret, emitgo.FunctionDeclaration{
				BindName: binding,
				BindType: emitgo.PointerType{Type: goName},
				Name:     emitgo.Identifier("Set") + name,
				Arguments: []emitgo.FunctionArgument{
					{Name: emitgo.Identifier("val"), Type: typ.goType},
				},
				ReturnType: emitgo.TypeBool,
				Body: emitgo.Block{
					typ.encoder(slice, emitgo.Identifier("val")),
					emitgo.ReturnExpression{Value: emitgo.RawStringf("true")},
				},
			})

			memberTypes[member.Name] = typ
		case *StaticArrayType:
			name := makeGoName(member.Name, true)

			// Generate a length method.
			ret = append(ret, emitgo.FunctionDeclaration{
				BindName:   binding,
				BindType:   goName,
				Name:       name + emitgo.Identifier("Length"),
				ReturnType: emitgo.TypeInt64,
				Body: emitgo.Block{
					emitgo.ReturnExpression{
						Value: emitgo.NumberLiteral(fmt.Sprintf("%d", typ.Count)),
					},
				},
			})

			switch member := typ.Member.(type) {
			case *BasicType:
				slice := emitgo.ArrayAccessRangeExpression{
					Target: binding,
					Start: emitgo.BinaryExpression{
						Lhs: emitgo.NumberLiteral(fmt.Sprintf("%d", currentOffset)),
						Op:  "+",
						Rhs: emitgo.BinaryExpression{
							Lhs: emitgo.Identifier("i"),
							Op:  "*",
							Rhs: emitgo.NumberLiteral(fmt.Sprintf("%d", member.size)),
						},
					},
					End: emitgo.BinaryExpression{
						Lhs: emitgo.NumberLiteral(fmt.Sprintf("%d", currentOffset)),
						Op:  "+",
						Rhs: emitgo.BinaryExpression{
							Lhs: emitgo.BinaryExpression{
								Lhs: emitgo.Identifier("i"),
								Op:  "+",
								Rhs: emitgo.NumberLiteral("1"),
							},
							Op:  "*",
							Rhs: emitgo.NumberLiteral(fmt.Sprintf("%d", member.size)),
						},
					},
				}

				// Generate a Getter
				ret = append(ret, emitgo.FunctionDeclaration{
					BindName: binding,
					BindType: goName,
					Name:     name,
					Arguments: []emitgo.FunctionArgument{
						{Name: emitgo.Identifier("i"), Type: emitgo.TypeInt},
					},
					ReturnType: emitgo.Tuple{member.goType, emitgo.TypeBool},
					Body: emitgo.Block{
						emitgo.IfStatement{
							Expr: emitgo.BinaryExpression{
								Lhs: emitgo.Identifier("i"),
								Op:  ">=",
								Rhs: emitgo.NumberLiteral(fmt.Sprintf("%d", typ.Count)),
							},
							Body: emitgo.Block{
								emitgo.ReturnExpression{
									Value: emitgo.List{emitgo.NumberLiteral("0"), emitgo.RawStringf("false")},
								},
							},
						},
						emitgo.ReturnExpression{
							Value: emitgo.List{member.decoder(slice), emitgo.RawStringf("true")},
						},
					},
				})

				// Generate a Setter
				ret = append(ret, emitgo.FunctionDeclaration{
					BindName: binding,
					BindType: emitgo.PointerType{Type: goName},
					Name:     emitgo.Identifier("Set") + name,
					Arguments: []emitgo.FunctionArgument{
						{Name: emitgo.Identifier("i"), Type: emitgo.TypeInt},
						{Name: emitgo.Identifier("val"), Type: member.goType},
					},
					ReturnType: emitgo.TypeBool,
					Body: emitgo.Block{
						emitgo.IfStatement{
							Expr: emitgo.BinaryExpression{
								Lhs: emitgo.Identifier("i"),
								Op:  ">=",
								Rhs: emitgo.NumberLiteral(fmt.Sprintf("%d", typ.Count)),
							},
							Body: emitgo.Block{
								emitgo.ReturnExpression{
									Value: emitgo.RawStringf("false"),
								},
							},
						},
						member.encoder(slice, emitgo.Identifier("val")),
						emitgo.ReturnExpression{Value: emitgo.RawStringf("true")},
					},
				})
			case *StructureType:
				slice := emitgo.ArrayAccessRangeExpression{
					Target: binding,
					Start: emitgo.BinaryExpression{
						Lhs: emitgo.NumberLiteral(fmt.Sprintf("%d", currentOffset)),
						Op:  "+",
						Rhs: emitgo.BinaryExpression{
							Lhs: emitgo.Identifier("i"),
							Op:  "*",
							Rhs: emitgo.NumberLiteral(fmt.Sprintf("%d", member.Size())),
						},
					},
					End: emitgo.BinaryExpression{
						Lhs: emitgo.NumberLiteral(fmt.Sprintf("%d", currentOffset)),
						Op:  "+",
						Rhs: emitgo.BinaryExpression{
							Lhs: emitgo.BinaryExpression{
								Lhs: emitgo.Identifier("i"),
								Op:  "+",
								Rhs: emitgo.NumberLiteral("1"),
							},
							Op:  "*",
							Rhs: emitgo.NumberLiteral(fmt.Sprintf("%d", member.Size())),
						},
					},
				}

				// Generate a Getter
				ret = append(ret, emitgo.FunctionDeclaration{
					BindName: binding,
					BindType: goName,
					Name:     name,
					Arguments: []emitgo.FunctionArgument{
						{Name: emitgo.Identifier("i"), Type: emitgo.TypeInt},
					},
					ReturnType: emitgo.Tuple{member.GoType(), emitgo.TypeBool},
					Body: emitgo.Block{
						emitgo.IfStatement{
							Expr: emitgo.BinaryExpression{
								Lhs: emitgo.Identifier("i"),
								Op:  ">=",
								Rhs: emitgo.NumberLiteral(fmt.Sprintf("%d", typ.Count)),
							},
							Body: emitgo.Block{
								emitgo.ReturnExpression{
									Value: emitgo.List{emitgo.RawStringf("%s{}", member.GoType()), emitgo.RawStringf("false")},
								},
							},
						},
						emitgo.ReturnExpression{
							Value: emitgo.List{member.Decoder(slice), emitgo.RawStringf("true")},
						},
					},
				})

				// Generate a Setter
				ret = append(ret, emitgo.FunctionDeclaration{
					BindName: binding,
					BindType: emitgo.PointerType{Type: goName},
					Name:     emitgo.Identifier("Set") + name,
					Arguments: []emitgo.FunctionArgument{
						{Name: emitgo.Identifier("i"), Type: emitgo.TypeInt},
						{Name: emitgo.Identifier("val"), Type: member.GoType()},
					},
					ReturnType: emitgo.TypeBool,
					Body: emitgo.Block{
						emitgo.IfStatement{
							Expr: emitgo.BinaryExpression{
								Lhs: emitgo.Identifier("i"),
								Op:  ">=",
								Rhs: emitgo.NumberLiteral(fmt.Sprintf("%d", typ.Count)),
							},
							Body: emitgo.Block{
								emitgo.ReturnExpression{
									Value: emitgo.RawStringf("false"),
								},
							},
						},
						member.Encoder(slice, emitgo.Identifier("val")),
						emitgo.ReturnExpression{Value: emitgo.RawStringf("true")},
					},
				})
			default:
				return nil, fmt.Errorf("member type not implemented: %T %+v", member, member)
			}
		case *StructureType:
			name := makeGoName(member.Name, true)

			slice := emitgo.ArrayAccessRangeExpression{
				Target: binding,
				Start:  emitgo.NumberLiteral(fmt.Sprintf("%d", currentOffset)),
				End:    emitgo.NumberLiteral(fmt.Sprintf("%d", currentOffset+typ.Size())),
			}

			if typ.name != "" {
				typeName := makeGoName(typ.name, true)

				// Generate a Getter
				ret = append(ret, emitgo.FunctionDeclaration{
					BindName:   binding,
					BindType:   goName,
					Name:       name,
					ReturnType: typeName,
					Body: emitgo.Block{
						emitgo.ReturnExpression{
							Value: emitgo.CallExpression{
								Target:    typeName,
								Arguments: []emitgo.Node{slice},
							},
						},
					},
				})
			} else {
				return nil, fmt.Errorf("anonymous structs not implemented")
			}
		default:
			return nil, fmt.Errorf("type not implemented: %T %+v", typ, typ)
		}

		currentOffset += member.Type.Size()
	}

	for _, field := range st.Fields {
		typ, ok := field.Type.(*BasicType)
		if !ok {
			return nil, fmt.Errorf("type not implemented: %T %+v", field.Type, field.Type)
		}

		fieldName := makeGoName(field.Name, true)

		lhsName := makeGoName(field.LhsName, true)
		rhsName := makeGoName(field.RhsName, true)

		lhsType, ok := memberTypes[field.LhsName]
		if !ok {
			return nil, fmt.Errorf("type for %s not found", field.LhsName)
		}
		rhsType, ok := memberTypes[field.RhsName]
		if !ok {
			return nil, fmt.Errorf("type for %s not found", field.RhsName)
		}

		// Generate a getter method.
		ret = append(ret, emitgo.FunctionDeclaration{
			BindName:   binding,
			BindType:   goName,
			Name:       fieldName,
			ReturnType: typ.goType,
			Body: emitgo.Block{
				emitgo.ReturnExpression{
					Value: emitgo.CallExpression{
						Target: "join_" + lhsType.goType + "_" + rhsType.goType,
						Arguments: []emitgo.Node{
							emitgo.CallExpression{Target: emitgo.MemberExpression{Target: binding, Name: lhsName}},
							emitgo.CallExpression{Target: emitgo.MemberExpression{Target: binding, Name: rhsName}},
						},
					},
				},
			},
		})

		// Generate a setter method.
		aBind := emitgo.Identifier("a")
		bBind := emitgo.Identifier("b")
		ret = append(ret, emitgo.FunctionDeclaration{
			BindName:   binding,
			BindType:   emitgo.PointerType{Type: goName},
			Name:       "Set" + fieldName,
			ReturnType: emitgo.TypeBool,
			Arguments: []emitgo.FunctionArgument{
				{Name: "val", Type: typ.goType},
			},
			Body: emitgo.Block{
				emitgo.VariableDeclaration{
					Name: emitgo.List{aBind, bBind},
					Value: emitgo.CallExpression{
						Target:    "split_" + lhsType.goType + "_" + rhsType.goType,
						Arguments: []emitgo.Node{emitgo.Identifier("val")},
					},
				},
				emitgo.CallExpression{
					Target: emitgo.MemberExpression{
						Target: binding,
						Name:   "Set" + lhsName,
					},
					Arguments: []emitgo.Node{aBind},
				},
				emitgo.CallExpression{
					Target: emitgo.MemberExpression{
						Target: binding,
						Name:   "Set" + rhsName,
					},
					Arguments: []emitgo.Node{bBind},
				},
				emitgo.ReturnExpression{Value: emitgo.RawStringf("true")},
			},
		})
	}

	// Generate a Size method.
	ret = append(ret, emitgo.FunctionDeclaration{
		BindName:   binding,
		BindType:   goName,
		Name:       emitgo.Identifier("Size"),
		ReturnType: emitgo.TypeInt64,
		Body: emitgo.Block{
			emitgo.ReturnExpression{Value: emitgo.NumberLiteral(fmt.Sprintf("%d", currentOffset))},
		},
	})

	// Generate a ReadAt method.
	ret = append(ret, emitgo.FunctionDeclaration{
		BindName: binding,
		BindType: goName,
		Name:     emitgo.Identifier("ReadAt"),
		Arguments: []emitgo.FunctionArgument{
			{Name: emitgo.Identifier("p"), Type: emitgo.ArrayType{Type: emitgo.TypeByte}},
			{Name: emitgo.Identifier("off"), Type: emitgo.TypeInt64},
		},
		ReturnType: emitgo.Tuple{
			emitgo.TypeInt,
			emitgo.TypeError,
		},
		Body: emitgo.Block{
			emitgo.RawStringf("if off > t.Size() { return 0, io.EOF }"),
			emitgo.RawStringf("return copy(p, t[off:]), nil"),
		},
	})

	// Generate a WriteAt method.
	ret = append(ret, emitgo.FunctionDeclaration{
		BindName: binding,
		BindType: emitgo.PointerType{Type: goName},
		Name:     emitgo.Identifier("WriteAt"),
		Arguments: []emitgo.FunctionArgument{
			{Name: emitgo.Identifier("p"), Type: emitgo.ArrayType{Type: emitgo.TypeByte}},
			{Name: emitgo.Identifier("off"), Type: emitgo.TypeInt64},
		},
		ReturnType: emitgo.Tuple{
			emitgo.TypeInt,
			emitgo.TypeError,
		},
		Body: emitgo.Block{
			emitgo.RawStringf("if off > t.Size() { return 0, io.EOF }"),
			emitgo.RawStringf("return copy(t[off:], p), nil"),
		},
	})

	fieldStrings = append(fieldStrings, emitgo.StringLiteral("}"))

	// Generate a String method.
	ret = append(ret, emitgo.FunctionDeclaration{
		BindName:   binding,
		BindType:   goName,
		Name:       emitgo.Identifier("String"),
		Arguments:  []emitgo.FunctionArgument{},
		ReturnType: emitgo.TypeString,
		Body: emitgo.Block{
			emitgo.ReturnExpression{Value: emitgo.CallExpression{
				Target: emitgo.RawStringf("strings.Join"),
				Arguments: []emitgo.Node{
					emitgo.ArrayExpression{
						Type:     emitgo.ArrayType{Type: emitgo.TypeString},
						Elements: fieldStrings,
					},
					emitgo.StringLiteral(" "),
				},
			}},
		},
	})

	ret = append(emitgo.Block{emitgo.TypeDeclaration{
		Name: goName,
		Type: emitgo.ArrayType{
			Elements: currentOffset,
			Type:     emitgo.TypeByte,
		},
	}}, ret...)

	return ret, nil
}

// Size implements Type.
func (st *StructureType) Size() int64 {
	var total int64 = 0

	for _, member := range st.Members {
		total += member.Type.Size()
	}

	return total
}

// IsBasic implements Type.
func (*StructureType) IsBasic() bool { return false }

var (
	_ Type         = &StructureType{}
	_ TopLevelType = &StructureType{}
)

type StaticArrayType struct {
	Count  int64
	Member Type
}

// Size implements Type.
func (a *StaticArrayType) Size() int64 {
	return a.Member.Size() * a.Count
}

var (
	_ Type = &StaticArrayType{}
)

type StructureCodeGenerator struct {
	types   map[string]Type
	pkgName string
}

func (gen *StructureCodeGenerator) evaluateExpression(expr parser.Expression) (Value, error) {
	switch expr := expr.(type) {
	case *parser.NumberLiteral:
		num, err := strconv.ParseInt(expr.Value, 0, 64)
		if err != nil {
			return nil, err
		}

		return NumberValue(num), nil
	default:
		return nil, fmt.Errorf("expression not implemented: %T %+v", expr, expr)
	}
}

func (gen *StructureCodeGenerator) getStructureField(name string, typ parser.Type, expr parser.Expression) (*StructureField, error) {
	switch expr := expr.(type) {
	case *parser.BinaryExpression:
		switch expr.Op {
		case parser.BinaryExpressionOpJoin:
			typ, err := gen.getType(typ)
			if err != nil {
				return nil, err
			}

			return &StructureField{
				Name:    name,
				Type:    typ,
				LhsName: expr.Lhs.(*parser.LocalReference).Name,
				RhsName: expr.Rhs.(*parser.LocalReference).Name,
			}, nil
		default:
			return nil, fmt.Errorf("field not implemented: %T %+v", expr, expr)
		}
	default:
		return nil, fmt.Errorf("field not implemented: %T %+v", expr, expr)
	}
}

func (gen *StructureCodeGenerator) getStructureType(typ *parser.StructureType) (*StructureType, error) {
	ret := &StructureType{}

	if typ.Parent != nil {
		parent, err := gen.getType(typ.Parent)
		if err != nil {
			return nil, err
		}

		ret.Parent = parent
	}

	for _, stmt := range typ.Body {
		switch stmt := stmt.(type) {
		case *parser.StructureMember:
			memberName := stmt.Name.Name

			memberType, err := gen.getType(stmt.Type)
			if err != nil {
				return nil, err
			}

			ret.Members = append(ret.Members, &StructureMember{
				Name: memberName,
				Type: memberType,
			})
		case *parser.StructureField:
			fieldName := stmt.Name.Name

			field, err := gen.getStructureField(fieldName, stmt.Type, stmt.Value)
			if err != nil {
				return nil, err
			}

			ret.Fields = append(ret.Fields, field)
		default:
			return nil, fmt.Errorf("statement not implemented: %T %+v", stmt, stmt)
		}
	}

	return ret, nil
}

func (gen *StructureCodeGenerator) getArrayType(typ *parser.ArrayType) (Type, error) {
	// Figure out if the array is static.
	if typ.Count != nil {
		countValue, err := gen.evaluateExpression(typ.Count)
		if err != nil {
			return nil, err
		}

		count, err := toInt64(countValue)
		if err != nil {
			return nil, err
		}

		member, err := gen.getType(typ.Member)
		if err != nil {
			return nil, err
		}

		return &StaticArrayType{
			Count:  count,
			Member: member,
		}, nil
	} else {
		return nil, fmt.Errorf("dynamic arrays not implemented")
	}
}

func (gen *StructureCodeGenerator) getType(typ parser.Type) (Type, error) {
	switch typ := typ.(type) {
	case *parser.StructureType:
		return gen.getStructureType(typ)
	case *parser.ArrayType:
		return gen.getArrayType(typ)
	case *parser.TypeReference:
		ref, ok := gen.types[typ.Name]
		if !ok {
			return nil, fmt.Errorf("type not found: %s", typ.Name)
		}

		return ref, nil
	default:
		return nil, fmt.Errorf("type not implemented: %T %+v", typ, typ)
	}
}

func (gen *StructureCodeGenerator) AddFile(file *parser.File) error {
	for _, decl := range file.Declarations {
		switch decl := decl.(type) {
		case *parser.TypeDeclaration:
			typ, err := gen.getType(decl.Type)
			if err != nil {
				return err
			}

			gen.types[decl.Name.Name] = typ
		default:
			return fmt.Errorf("declaration not implemented: %T %+v", decl, decl)
		}
	}

	return nil
}

// WriteTo implements io.WriterTo.
func (gen *StructureCodeGenerator) WriteTo(w io.Writer) (n int64, err error) {
	var ret emitgo.File

	ret = append(ret, emitgo.PackageDeclaration(gen.pkgName))

	ret = append(ret, emitgo.ImportDeclaration{Name: "encoding/binary"})
	ret = append(ret, emitgo.ImportDeclaration{Name: "io"})
	ret = append(ret, emitgo.ImportDeclaration{Name: "strings"})
	ret = append(ret, emitgo.ImportDeclaration{Name: "fmt"})

	// Sort the top level elements.
	var topLevel []string

	for name, typ := range gen.types {
		if _, ok := typ.(TopLevelType); ok {
			topLevel = append(topLevel, name)
		}
	}

	slices.Sort(topLevel)

	// Set all the names.
	for _, name := range topLevel {
		if err := gen.types[name].(TopLevelType).SetName(name); err != nil {
			return -1, err
		}
	}

	for _, name := range topLevel {
		generated, err := gen.types[name].(TopLevelType).GenerateCode()
		if err != nil {
			return -1, err
		}

		ret = append(ret, generated...)
	}

	return ret.EmitGo(w)
}

var (
	_ io.WriterTo = &StructureCodeGenerator{}
)

func makeBasicUnsignedType(size int64, goName emitgo.BasicType, name string, little bool) Type {
	endian := "BigEndian"
	if little {
		endian = "LittleEndian"
	}

	return &BasicType{
		size:   size,
		goType: goName,
		decoder: func(slice emitgo.Node) emitgo.Node {
			return emitgo.CallExpression{
				Target:    emitgo.RawStringf("binary.%s.%s", endian, name),
				Arguments: []emitgo.Node{slice},
			}
		},
		encoder: func(target, value emitgo.Node) emitgo.Node {
			return emitgo.CallExpression{
				Target:    emitgo.RawStringf("binary.%s.Put%s", endian, name),
				Arguments: []emitgo.Node{target, value},
			}
		},
	}
}

func toSigned(typ emitgo.BasicType) emitgo.BasicType {
	switch typ {
	case emitgo.TypeInt16:
		return emitgo.TypeUInt16
	case emitgo.TypeInt32:
		return emitgo.TypeUInt32
	case emitgo.TypeInt64:
		return emitgo.TypeUInt64
	default:
		panic(typ)
	}
}

func makeBasicSignedType(size int64, goName emitgo.BasicType, name string, little bool) Type {
	endian := "BigEndian"
	if little {
		endian = "LittleEndian"
	}

	return &BasicType{
		size:   size,
		goType: goName,
		decoder: func(slice emitgo.Node) emitgo.Node {
			return emitgo.CallExpression{
				Target: goName,
				Arguments: []emitgo.Node{
					emitgo.CallExpression{
						Target:    emitgo.RawStringf("binary.%s.%s", endian, name),
						Arguments: []emitgo.Node{slice},
					},
				},
			}
		},
		encoder: func(target, value emitgo.Node) emitgo.Node {
			return emitgo.CallExpression{
				Target: emitgo.RawStringf("binary.%s.Put%s", endian, name),
				Arguments: []emitgo.Node{
					target,
					emitgo.CallExpression{
						Target:    toSigned(goName),
						Arguments: []emitgo.Node{value},
					},
				},
			}
		},
	}
}

func NewStructureCodeGenerator(pkgName string) *StructureCodeGenerator {
	ret := &StructureCodeGenerator{
		types:   make(map[string]Type),
		pkgName: pkgName,
	}

	ret.types["byte"] = &BasicType{
		size:   1,
		goType: emitgo.TypeByte,
		decoder: func(slice emitgo.Node) emitgo.Node {
			return emitgo.ArrayAccessExpression{Target: slice, Index: emitgo.NumberLiteral("0")}
		},
		encoder: func(target, value emitgo.Node) emitgo.Node {
			return emitgo.AssignExpression{
				Target: emitgo.ArrayAccessExpression{Target: target, Index: emitgo.NumberLiteral("0")},
				Expr:   value,
			}
		},
	}

	ret.types["string"] = &BasicType{
		size:   1,
		goType: emitgo.TypeByte,
		decoder: func(slice emitgo.Node) emitgo.Node {
			return emitgo.ArrayAccessExpression{Target: slice, Index: emitgo.NumberLiteral("0")}
		},
		encoder: func(target, value emitgo.Node) emitgo.Node {
			return emitgo.AssignExpression{
				Target: emitgo.ArrayAccessExpression{Target: target, Index: emitgo.NumberLiteral("0")},
				Expr:   value,
			}
		},
	}

	ret.types["u8"] = &BasicType{
		size:   1,
		goType: emitgo.TypeUInt8,
		decoder: func(slice emitgo.Node) emitgo.Node {
			return emitgo.ArrayAccessExpression{Target: slice, Index: emitgo.NumberLiteral("0")}
		},
		encoder: func(target, value emitgo.Node) emitgo.Node {
			return emitgo.AssignExpression{
				Target: emitgo.ArrayAccessExpression{Target: target, Index: emitgo.NumberLiteral("0")},
				Expr:   value,
			}
		},
	}

	ret.types["u16_le"] = makeBasicUnsignedType(2, emitgo.TypeUInt16, "Uint16", true)
	ret.types["u16_be"] = makeBasicUnsignedType(2, emitgo.TypeUInt16, "Uint16", false)
	ret.types["u32_le"] = makeBasicUnsignedType(4, emitgo.TypeUInt32, "Uint32", true)
	ret.types["u32_be"] = makeBasicUnsignedType(4, emitgo.TypeUInt32, "Uint32", false)
	ret.types["u64_le"] = makeBasicUnsignedType(8, emitgo.TypeUInt64, "Uint64", true)
	ret.types["u64_be"] = makeBasicUnsignedType(8, emitgo.TypeUInt64, "Uint64", false)

	ret.types["i8"] = &BasicType{
		size:   1,
		goType: emitgo.TypeInt8,
		decoder: func(slice emitgo.Node) emitgo.Node {
			return emitgo.CallExpression{
				Target: emitgo.TypeInt8,
				Arguments: []emitgo.Node{
					emitgo.ArrayAccessExpression{Target: slice, Index: emitgo.NumberLiteral("0")},
				},
			}
		},
		encoder: func(target, value emitgo.Node) emitgo.Node {
			return emitgo.AssignExpression{
				Target: emitgo.ArrayAccessExpression{Target: target, Index: emitgo.NumberLiteral("0")},
				Expr: emitgo.CallExpression{
					Target:    emitgo.TypeUInt8,
					Arguments: []emitgo.Node{value},
				},
			}
		},
	}

	ret.types["i16_le"] = makeBasicSignedType(2, emitgo.TypeInt16, "Uint16", true)
	ret.types["i16_be"] = makeBasicSignedType(2, emitgo.TypeInt16, "Uint16", false)
	ret.types["i32_le"] = makeBasicSignedType(4, emitgo.TypeInt32, "Uint32", true)
	ret.types["i32_be"] = makeBasicSignedType(4, emitgo.TypeInt32, "Uint32", false)
	ret.types["i64_le"] = makeBasicSignedType(8, emitgo.TypeInt64, "Uint64", true)
	ret.types["i64_be"] = makeBasicSignedType(8, emitgo.TypeInt64, "Uint64", false)

	return ret
}

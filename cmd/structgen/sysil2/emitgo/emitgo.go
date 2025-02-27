package emitgo

import (
	"fmt"
	"io"
	"strings"
)

type Node interface {
	EmitGo() (string, error)
}

type Comment string

func (n Comment) EmitGo() (string, error) {
	return fmt.Sprintf("// %s", n), nil
}

type PackageDeclaration string

// EmitGo implements Node.
func (n PackageDeclaration) EmitGo() (string, error) {
	return fmt.Sprintf("package %s", n), nil
}

type ImportDeclaration struct {
	Name  string
	Alias string
}

// EmitGo implements Node.
func (n ImportDeclaration) EmitGo() (string, error) {
	return fmt.Sprintf("import %s \"%s\"", n.Alias, n.Name), nil
}

type RawString string

// EmitGo implements Node.
func (n RawString) EmitGo() (string, error) {
	return string(n), nil
}

func RawStringf(format string, a ...any) RawString {
	return RawString(fmt.Sprintf(format, a...))
}

type Identifier string

func (n Identifier) String() string {
	return string(n)
}

// EmitGo implements Node.
func (n Identifier) EmitGo() (string, error) {
	return string(n), nil
}

type TypeDeclaration struct {
	Name Identifier
	Type Node
}

// EmitGo implements Node.
func (n TypeDeclaration) EmitGo() (string, error) {
	typ, err := n.Type.EmitGo()
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("type %s %s", n.Name, typ), nil
}

type ConstDeclaration struct {
	Name  Identifier
	Type  Node
	Value Node
}

// EmitGo implements Node.
func (n ConstDeclaration) EmitGo() (string, error) {
	typ, err := n.Type.EmitGo()
	if err != nil {
		return "", err
	}

	var value = ""

	if n.Value != nil {
		value, err = n.Value.EmitGo()
		if err != nil {
			return "", err
		}
	}

	if value != "" {
		return fmt.Sprintf("const %s %s = %s", n.Name, typ, value), nil
	} else {
		return fmt.Sprintf("const %s %s", n.Name, typ), nil
	}
}

type VariableDeclaration struct {
	Name  Node
	Type  Node
	Value Node
}

// EmitGo implements Node.
func (n VariableDeclaration) EmitGo() (string, error) {
	name, err := n.Name.EmitGo()
	if err != nil {
		return "", err
	}

	var typ = ""

	if n.Type != nil {
		typ, err = n.Type.EmitGo()
		if err != nil {
			return "", err
		}
	}

	var value = ""

	if n.Value != nil {
		value, err = n.Value.EmitGo()
		if err != nil {
			return "", err
		}
	}

	if value != "" {
		return fmt.Sprintf("var %s %s = %s", name, typ, value), nil
	} else {
		return fmt.Sprintf("var %s %s", name, typ), nil
	}
}

type InterfaceDeclaration struct {
}

// EmitGo implements Node.
func (InterfaceDeclaration) EmitGo() (string, error) {
	return "interface{}", nil
}

type StructDeclaration []Node

// EmitGo implements Node.
func (n StructDeclaration) EmitGo() (string, error) {
	ret := "struct {\n"

	for _, node := range n {
		s, err := node.EmitGo()
		if err != nil {
			return "", err
		}

		ret += s + "\n"
	}

	ret += "}\n"

	return ret, nil
}

type StructInheritDeclaration struct {
	Parent Node
}

// EmitGo implements Node.
func (n StructInheritDeclaration) EmitGo() (string, error) {
	parent, err := n.Parent.EmitGo()
	if err != nil {
		return "", err
	}

	return parent + "\n", nil
}

type StructMember struct {
	Name Identifier
	Type Node
}

// EmitGo implements Node.
func (n StructMember) EmitGo() (string, error) {
	typ, err := n.Type.EmitGo()
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%s %s", n.Name, typ), nil
}

type ReturnExpression struct {
	Value Node
}

// EmitGo implements Node.
func (n ReturnExpression) EmitGo() (string, error) {
	if n.Value != nil {
		val, err := n.Value.EmitGo()
		if err != nil {
			return "", err
		}

		return fmt.Sprintf("return %s", val), nil
	} else {
		return "return", nil
	}
}

type Block []Node

// EmitGo implements Node.
func (n Block) EmitGo() (string, error) {
	ret := ""
	for _, node := range n {
		s, err := node.EmitGo()
		if err != nil {
			return "", err
		}

		ret += s + "\n"
	}
	return fmt.Sprintf("{%s}", ret), nil
}

type FunctionArgument struct {
	Name Identifier
	Type Node
}

// EmitGo implements Node.
func (n FunctionArgument) EmitGo() (string, error) {
	typ, err := n.Type.EmitGo()
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%s %s", n.Name, typ), nil
}

type FunctionDeclaration struct {
	BindName   Identifier
	BindType   Node
	Name       Identifier
	Arguments  []FunctionArgument
	ReturnType Node
	Body       Block
}

// EmitGo implements Node.
func (n FunctionDeclaration) EmitGo() (string, error) {
	s := "func"
	if n.BindType != nil {
		typ, err := n.BindType.EmitGo()
		if err != nil {
			return "", err
		}

		s += fmt.Sprintf("(%s %s)", n.BindName, typ)
	}

	s += fmt.Sprintf(" %s", n.Name)

	var args []string
	for _, arg := range n.Arguments {
		s, err := arg.EmitGo()
		if err != nil {
			return "", err
		}

		args = append(args, s)
	}
	s += fmt.Sprintf("(%s)", strings.Join(args, ", "))

	if n.ReturnType != nil {
		typ, err := n.ReturnType.EmitGo()
		if err != nil {
			return "", err
		}

		s += fmt.Sprintf(" %s ", typ)
	}

	body, err := n.Body.EmitGo()
	if err != nil {
		return "", err
	}

	s += body

	return s, nil
}

type PointerType struct {
	Type Node
}

// EmitGo implements Node.
func (n PointerType) EmitGo() (string, error) {
	typ, err := n.Type.EmitGo()
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("*%s", typ), nil
}

type ArrayType struct {
	Elements int64
	Type     Node
}

// EmitGo implements Node.
func (n ArrayType) EmitGo() (string, error) {
	typ, err := n.Type.EmitGo()
	if err != nil {
		return "", err
	}

	if n.Elements < 1 {
		return fmt.Sprintf("[]%s", typ), nil
	} else {
		return fmt.Sprintf("[%d]%s", n.Elements, typ), nil
	}
}

type BasicType string

// EmitGo implements Node.
func (n BasicType) EmitGo() (string, error) {
	return string(n), nil
}

const (
	TypeString = BasicType("string")
	TypeBool   = BasicType("bool")
	TypeByte   = BasicType("byte")
	TypeInt    = BasicType("int")
	TypeInt8   = BasicType("int8")
	TypeInt16  = BasicType("int16")
	TypeInt32  = BasicType("int32")
	TypeInt64  = BasicType("int64")
	TypeUInt8  = BasicType("uint8")
	TypeUInt16 = BasicType("uint16")
	TypeUInt32 = BasicType("uint32")
	TypeUInt64 = BasicType("uint64")
	TypeError  = BasicType("error")
)

type SwitchCase struct {
	Expr Node
	Body Block
}

// EmitGo implements Node.
func (n SwitchCase) EmitGo() (string, error) {
	expr, err := n.Expr.EmitGo()
	if err != nil {
		return "", err
	}

	body, err := n.Body.EmitGo()
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("case %s: %s", expr, body), nil
}

type SwitchDefaultCase struct {
	Body Block
}

// EmitGo implements Node.
func (n SwitchDefaultCase) EmitGo() (string, error) {
	body, err := n.Body.EmitGo()
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("default: %s", body), nil
}

type SwitchStatement struct {
	Expr  Node
	Cases Block
}

// EmitGo implements Node.
func (n SwitchStatement) EmitGo() (string, error) {
	expr, err := n.Expr.EmitGo()
	if err != nil {
		return "", err
	}

	cases, err := n.Cases.EmitGo()
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("switch %s %s", expr, cases), nil
}

type StringLiteral string

// EmitGo implements Node.
func (n StringLiteral) EmitGo() (string, error) {
	return fmt.Sprintf("\"%s\"", n), nil
}

type NumberLiteral string

// EmitGo implements Node.
func (n NumberLiteral) EmitGo() (string, error) {
	return string(n), nil
}

type ForLoopStatement struct {
	Initial     Node
	Termination Node
	Step        Node
	Body        Block
}

// EmitGo implements Node.
func (n ForLoopStatement) EmitGo() (string, error) {
	init, err := n.Initial.EmitGo()
	if err != nil {
		return "", err
	}

	term, err := n.Termination.EmitGo()
	if err != nil {
		return "", err
	}

	step, err := n.Step.EmitGo()
	if err != nil {
		return "", err
	}

	body, err := n.Body.EmitGo()
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("for %s; %s; %s %s", init, term, step, body), nil
}

type CallExpression struct {
	Target    Node
	Arguments []Node
}

// EmitGo implements Node.
func (n CallExpression) EmitGo() (string, error) {
	target, err := n.Target.EmitGo()
	if err != nil {
		return "", err
	}

	var args []string

	for _, arg := range n.Arguments {
		s, err := arg.EmitGo()
		if err != nil {
			return "", err
		}

		args = append(args, s)
	}

	return fmt.Sprintf("%s(%s)", target, strings.Join(args, ", ")), nil
}

type CastExpression struct {
	Target Node
	Source Node
}

// EmitGo implements Node.
func (n CastExpression) EmitGo() (string, error) {
	target, err := n.Target.EmitGo()
	if err != nil {
		return "", err
	}

	source, err := n.Source.EmitGo()
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%s(%s)", target, source), nil
}

type AssignExpression struct {
	Target Node
	Expr   Node
}

// EmitGo implements Node.
func (n AssignExpression) EmitGo() (string, error) {
	target, err := n.Target.EmitGo()
	if err != nil {
		return "", err
	}

	expr, err := n.Expr.EmitGo()
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%s = %s", target, expr), nil
}

type ReturnsErrorExpression struct {
	Expr Node
}

// EmitGo implements Node.
func (n ReturnsErrorExpression) EmitGo() (string, error) {
	return n.Expr.EmitGo()
}

type Tuple []Node

// EmitGo implements Node.
func (n Tuple) EmitGo() (string, error) {
	var vals []string
	for _, val := range n {
		s, err := val.EmitGo()
		if err != nil {
			return "", err
		}

		vals = append(vals, s)
	}

	return fmt.Sprintf("(%s)", strings.Join(vals, ", ")), nil
}

type List []Node

// EmitGo implements Node.
func (n List) EmitGo() (string, error) {
	var vals []string
	for _, val := range n {
		s, err := val.EmitGo()
		if err != nil {
			return "", err
		}

		vals = append(vals, s)
	}

	return strings.Join(vals, ", "), nil
}

type IfStatement struct {
	Expr Node
	Body Block
	Else Block
}

// EmitGo implements Node.
func (n IfStatement) EmitGo() (string, error) {
	expr, err := n.Expr.EmitGo()
	if err != nil {
		return "", err
	}

	body, err := n.Body.EmitGo()
	if err != nil {
		return "", err
	}

	if n.Else != nil {
		elseBlock, err := n.Else.EmitGo()
		if err != nil {
			return "", err
		}

		return fmt.Sprintf("if %s %s else %s", expr, body, elseBlock), nil
	} else {
		return fmt.Sprintf("if %s %s", expr, body), nil
	}
}

type ArrayExpression struct {
	Type     Node
	Elements []Node
}

// EmitGo implements Node.
func (n ArrayExpression) EmitGo() (string, error) {
	typ, err := n.Type.EmitGo()
	if err != nil {
		return "", err
	}

	var elements []string
	for _, element := range n.Elements {
		s, err := element.EmitGo()
		if err != nil {
			return "", err
		}

		elements = append(elements, s)
	}

	return fmt.Sprintf("%s{%s}", typ, strings.Join(elements, ", ")), nil
}

type BinaryExpression struct {
	Lhs Node
	Op  string
	Rhs Node
}

// EmitGo implements Node.
func (n BinaryExpression) EmitGo() (string, error) {
	lhs, err := n.Lhs.EmitGo()
	if err != nil {
		return "", err
	}

	rhs, err := n.Rhs.EmitGo()
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("(%s %s %s)", lhs, n.Op, rhs), nil
}

type MemberExpression struct {
	Target Node
	Name   Node
}

// EmitGo implements Node.
func (n MemberExpression) EmitGo() (string, error) {
	target, err := n.Target.EmitGo()
	if err != nil {
		return "", err
	}

	name, err := n.Name.EmitGo()
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%s.%s", target, name), nil
}

type ArrayAccessExpression struct {
	Target Node
	Index  Node
}

// EmitGo implements Node.
func (n ArrayAccessExpression) EmitGo() (string, error) {
	target, err := n.Target.EmitGo()
	if err != nil {
		return "", err
	}

	index, err := n.Index.EmitGo()
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%s[%s]", target, index), nil
}

type ArrayAccessRangeExpression struct {
	Target Node
	Start  Node
	End    Node
}

// EmitGo implements Node.
func (n ArrayAccessRangeExpression) EmitGo() (string, error) {
	target, err := n.Target.EmitGo()
	if err != nil {
		return "", err
	}

	var start, end string

	if n.Start != nil {
		start, err = n.Start.EmitGo()
		if err != nil {
			return "", err
		}
	}

	if n.End != nil {
		end, err = n.End.EmitGo()
		if err != nil {
			return "", err
		}
	}

	return fmt.Sprintf("%s[%s:%s]", target, start, end), nil
}

var (
	_ Node = Comment("")
	_ Node = PackageDeclaration("")
	_ Node = ImportDeclaration{}
	_ Node = RawString("")
	_ Node = Identifier("")
	_ Node = TypeDeclaration{}
	_ Node = ConstDeclaration{}
	_ Node = VariableDeclaration{}
	_ Node = InterfaceDeclaration{}
	_ Node = StructDeclaration{}
	_ Node = StructInheritDeclaration{}
	_ Node = StructMember{}
	_ Node = ReturnExpression{}
	_ Node = Block{}
	_ Node = FunctionArgument{}
	_ Node = FunctionDeclaration{}
	_ Node = PointerType{}
	_ Node = ArrayType{}
	_ Node = BasicType("")
	_ Node = SwitchCase{}
	_ Node = SwitchDefaultCase{}
	_ Node = SwitchStatement{}
	_ Node = StringLiteral("")
	_ Node = NumberLiteral("")
	_ Node = ForLoopStatement{}
	_ Node = CallExpression{}
	_ Node = CastExpression{}
	_ Node = AssignExpression{}
	_ Node = ReturnsErrorExpression{}
	_ Node = Tuple{}
	_ Node = List{}
	_ Node = IfStatement{}
	_ Node = BinaryExpression{}
	_ Node = MemberExpression{}
	_ Node = ArrayAccessExpression{}
	_ Node = ArrayAccessRangeExpression{}
)

type File []Node

func (f File) EmitGo(w io.Writer) (int64, error) {
	nodes := append([]Node{
		Comment("autogenerated by structGenerator. DO NOT EDIT."),
	}, f...)

	var total int64 = 0

	for _, node := range nodes {
		s, err := node.EmitGo()
		if err != nil {
			return 0, err
		}

		n, err := fmt.Fprintf(w, "%s\n", s)
		if err != nil {
			return 0, err
		}

		total += int64(n)
	}

	return total, nil
}

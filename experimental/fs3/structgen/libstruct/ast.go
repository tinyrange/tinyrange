package libstruct

import "fmt"

type Node interface {
	tagNode()
}

type Declaration interface {
	Node

	tagDeclaration()
}

type TypeInstance interface {
	Node

	tagTypeInstance()

	String() string
}

type Expression interface {
	Node

	tagExpression()
}

type StructMember interface {
	Node

	tagStructMember()
}

type baseNode struct {
}

func (n *baseNode) tagNode() {}

type baseDeclaration struct {
	baseNode
}

func (d *baseDeclaration) tagDeclaration() {}

type baseTypeInstance struct {
	baseNode
}

func (t *baseTypeInstance) tagTypeInstance() {}

type baseExpression struct {
	baseNode
}

func (e *baseExpression) tagExpression() {}

type baseStructMember struct {
	baseNode
}

func (m *baseStructMember) tagStructMember() {}

type File struct {
	baseNode

	Declarations []Declaration
}

type TypeDeclaration struct {
	baseDeclaration

	Name      string
	InnerType TypeInstance
}

type EnumMember struct {
	baseNode

	Name  string
	Value Expression
}

type EnumDeclaration struct {
	baseTypeInstance

	ValueType TypeInstance
	Members   []*EnumMember
}

// String implements TypeInstance.
func (e *EnumDeclaration) String() string {
	return "enum"
}

type StructOrUnionMember struct {
	baseStructMember

	Name      string
	ValueType TypeInstance
}

type StructConstMember struct {
	baseStructMember

	Name  string
	Value Expression
}

type StructSizeMember struct {
	baseStructMember

	Size Expression
}

type StructDeclaration struct {
	baseTypeInstance

	Dynamic bool
	Members []StructMember
}

// String implements TypeInstance.
func (s *StructDeclaration) String() string {
	return "struct"
}

type UnionDeclaration struct {
	baseTypeInstance

	Members []StructMember
}

// String implements TypeInstance.
func (u *UnionDeclaration) String() string {
	return "union"
}

type BitsetDeclaration struct {
	baseTypeInstance

	ValueType TypeInstance
	Members   []string
}

// String implements TypeInstance.
func (b *BitsetDeclaration) String() string {
	return "bitset"
}

type TypeInstanceBuiltin string

// String implements TypeInstance.
func (t TypeInstanceBuiltin) String() string {
	return string(t)
}

func (t TypeInstanceBuiltin) tagNode()         {}
func (t TypeInstanceBuiltin) tagTypeInstance() {}

const (
	TypeInstanceBuiltinUint8    TypeInstanceBuiltin = "u8"
	TypeInstanceBuiltinUint16LE TypeInstanceBuiltin = "u16_le"
	TypeInstanceBuiltinUint32LE TypeInstanceBuiltin = "u32_le"
	TypeInstanceBuiltinUint64LE TypeInstanceBuiltin = "u64_le"
	TypeInstanceBuiltinUint16BE TypeInstanceBuiltin = "u16_be"
	TypeInstanceBuiltinUint32BE TypeInstanceBuiltin = "u32_be"
	TypeInstanceBuiltinUint64BE TypeInstanceBuiltin = "u64_be"
	TypeInstanceBuiltinInt8     TypeInstanceBuiltin = "i8"
	TypeInstanceBuiltinInt16LE  TypeInstanceBuiltin = "i16_le"
	TypeInstanceBuiltinInt32LE  TypeInstanceBuiltin = "i32_le"
	TypeInstanceBuiltinInt64LE  TypeInstanceBuiltin = "i64_le"
	TypeInstanceBuiltinInt16BE  TypeInstanceBuiltin = "i16_be"
	TypeInstanceBuiltinInt32BE  TypeInstanceBuiltin = "i32_be"
	TypeInstanceBuiltinInt64BE  TypeInstanceBuiltin = "i64_be"
	TypeInstanceBuiltinChar     TypeInstanceBuiltin = "char"
)

type TypeInstanceReference string

// String implements TypeInstance.
func (t TypeInstanceReference) String() string {
	return string(t)
}

func (t TypeInstanceReference) tagNode()         {}
func (t TypeInstanceReference) tagTypeInstance() {}

type TypeInstanceArray struct {
	baseTypeInstance

	Size        Expression
	ElementType TypeInstance
}

// String implements TypeInstance.
func (t *TypeInstanceArray) String() string {
	return fmt.Sprintf("[%s]%s", t.Size, t.ElementType)
}

type NumberLiteralExpression string

func (n NumberLiteralExpression) tagNode()       {}
func (n NumberLiteralExpression) tagExpression() {}

type VariableReferenceExpression string

func (v VariableReferenceExpression) tagNode()       {}
func (v VariableReferenceExpression) tagExpression() {}

type BinaryOp int

const (
	BinaryOpInvalid BinaryOp = iota
	BinaryOpPlus
	BinaryOpMinus
)

func (o BinaryOp) String() string {
	switch o {
	case BinaryOpMinus:
		return "-"
	default:
		return fmt.Sprintf("invalid<%d>", o)
	}
}

type BinaryExpression struct {
	baseExpression

	Op  BinaryOp
	Lhs Expression
	Rhs Expression
}

func (b *BinaryExpression) String() string {
	return fmt.Sprintf("(%s %s %s)", b.Lhs, b.Op, b.Rhs)
}

var (
	_ Node         = &File{}
	_ Declaration  = &TypeDeclaration{}
	_ Node         = &EnumMember{}
	_ TypeInstance = &EnumDeclaration{}
	_ StructMember = &StructOrUnionMember{}
	_ StructMember = &StructConstMember{}
	_ TypeInstance = &StructDeclaration{}
	_ TypeInstance = &UnionDeclaration{}
	_ TypeInstance = &BitsetDeclaration{}
	_ TypeInstance = TypeInstanceBuiltin("")
	_ TypeInstance = TypeInstanceReference("")
	_ TypeInstance = &TypeInstanceArray{}
	_ Expression   = NumberLiteralExpression("")
	_ Expression   = VariableReferenceExpression("")
	_ Expression   = &BinaryExpression{}
)

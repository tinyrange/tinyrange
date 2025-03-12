package main

type node interface {
	tagNode()
}

type declaration interface {
	node

	tagDeclaration()
}

type declaredType interface {
	node

	tagDeclaredType()
}

type typeInstance interface {
	node

	tagTypeInstance()
}

type expression interface {
	node

	tagExpression()
}

type structMember interface {
	node

	tagStructMember()
}

type baseNode struct {
}

func (n *baseNode) tagNode() {}

type baseDeclaration struct {
	baseNode
}

func (d *baseDeclaration) tagDeclaration() {}

type baseDeclaredType struct {
	baseNode
}

func (t *baseDeclaredType) tagDeclaredType() {}

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

type file struct {
	baseNode

	declarations []declaration
}

type typeDeclaration struct {
	baseDeclaration

	name      string
	innerType declaredType
}

type enumMember struct {
	baseNode

	name  string
	value expression
}

type enumDeclaration struct {
	baseDeclaredType

	valueType typeInstance
	members   []*enumMember
}

type structOrUnionMember struct {
	baseStructMember

	name      string
	valueType typeInstance
}

type structConstMember struct {
	baseStructMember

	name  string
	value expression
}

type structDeclaration struct {
	baseDeclaredType

	dynamic bool
	members []structMember
}

type unionDeclaration struct {
	baseDeclaredType

	members []*structOrUnionMember
}

type bitsetDeclaration struct {
	baseDeclaredType

	valueType typeInstance
	members   []string
}

type typeInstanceBuiltin string

func (t typeInstanceBuiltin) tagNode()         {}
func (t typeInstanceBuiltin) tagTypeInstance() {}

const (
	typeInstanceBuiltinUint8 typeInstanceBuiltin = "u8"

	typeInstanceBuiltinUint16LE typeInstanceBuiltin = "u16_le"
	typeInstanceBuiltinUint32LE typeInstanceBuiltin = "u32_le"
	typeInstanceBuiltinUint64LE typeInstanceBuiltin = "u64_le"

	typeInstanceBuiltinUint16BE typeInstanceBuiltin = "u16_be"
	typeInstanceBuiltinUint32BE typeInstanceBuiltin = "u32_be"
	typeInstanceBuiltinUint64BE typeInstanceBuiltin = "u64_be"

	typeInstanceBuiltinInt8 typeInstanceBuiltin = "i8"

	typeInstanceBuiltinInt16LE typeInstanceBuiltin = "i16_le"
	typeInstanceBuiltinInt32LE typeInstanceBuiltin = "i32_le"
	typeInstanceBuiltinInt64LE typeInstanceBuiltin = "i64_le"

	typeInstanceBuiltinInt16BE typeInstanceBuiltin = "i16_be"
	typeInstanceBuiltinInt32BE typeInstanceBuiltin = "i32_be"
	typeInstanceBuiltinInt64BE typeInstanceBuiltin = "i64_be"

	typeInstanceBuiltinChar typeInstanceBuiltin = "char"
)

type typeInstanceReference string

func (t typeInstanceReference) tagNode()         {}
func (t typeInstanceReference) tagTypeInstance() {}

type typeInstanceArray struct {
	baseTypeInstance

	size        expression
	elementType typeInstance
}

type numberLiteralExpression string

func (n numberLiteralExpression) tagNode()       {}
func (n numberLiteralExpression) tagExpression() {}

type variableReferenceExpression string

func (v variableReferenceExpression) tagNode()       {}
func (v variableReferenceExpression) tagExpression() {}

type binaryOp int

const (
	binaryOpInvalid binaryOp = iota
	binaryOpMinus
)

type binaryExpression struct {
	baseExpression

	op  binaryOp
	lhs expression
	rhs expression
}

var (
	_ node         = &file{}
	_ declaration  = &typeDeclaration{}
	_ node         = &enumMember{}
	_ declaredType = &enumDeclaration{}
	_ structMember = &structOrUnionMember{}
	_ structMember = &structConstMember{}
	_ declaredType = &structDeclaration{}
	_ declaredType = &unionDeclaration{}
	_ typeInstance = typeInstanceBuiltin("")
	_ typeInstance = typeInstanceReference("")
	_ typeInstance = &typeInstanceArray{}
	_ expression   = numberLiteralExpression("")
	_ expression   = variableReferenceExpression("")
	_ expression   = &binaryExpression{}
)

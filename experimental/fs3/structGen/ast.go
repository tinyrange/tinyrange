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
	baseNode

	name      string
	valueType typeInstance
}

type structDeclaration struct {
	baseDeclaredType

	dynamic bool
	members []*structOrUnionMember
}

type unionDeclaration struct {
	baseDeclaredType

	members []*structOrUnionMember
}

type bitsetDeclaration struct {
	baseDeclaredType

	members []string
}

type typeInstanceBuiltin string

func (t typeInstanceBuiltin) tagNode()         {}
func (t typeInstanceBuiltin) tagTypeInstance() {}

const (
	typeInstanceBuiltinUint8 typeInstanceBuiltin = "u8"
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
	_ node         = &structOrUnionMember{}
	_ declaredType = &structDeclaration{}
	_ declaredType = &unionDeclaration{}
	_ typeInstance = typeInstanceBuiltin("")
	_ typeInstance = typeInstanceReference("")
	_ typeInstance = &typeInstanceArray{}
	_ expression   = numberLiteralExpression("")
	_ expression   = variableReferenceExpression("")
	_ expression   = &binaryExpression{}
)

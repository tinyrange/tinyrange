package parser

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
)

// AST Nodes

type Node interface {
	tagNode()
}

type Declaration interface {
	tagDeclaration()
}

type Expression interface {
	tagExpression()
}

type Type interface {
	tagType()
}

type Statement interface {
	tagStatement()
}

type MatchArm interface {
	tagMatchArm()
}

type baseNode struct{}

// TagNode implements Node.
func (*baseNode) tagNode() { panic("unimplemented") }

type baseDeclaration struct{}

// tagDeclaration implements Declaration.
func (*baseDeclaration) tagDeclaration() { panic("unimplemented") }

type baseExpression struct{}

// tagExpression implements Expression.
func (*baseExpression) tagExpression() { panic("unimplemented") }

type baseType struct{}

// tagType implements Type.
func (*baseType) tagType() { panic("unimplemented") }

type baseStatement struct{}

// tagStatement implements Statement.
func (*baseStatement) tagStatement() { panic("unimplemented") }

type baseMatchArm struct{}

// tagMatchArm implements MatchArm.
func (*baseMatchArm) tagMatchArm() { panic("unimplemented") }

var (
	_ Node        = &baseNode{}
	_ Declaration = &baseDeclaration{}
	_ Expression  = &baseExpression{}
	_ Type        = &baseType{}
	_ Statement   = &baseStatement{}
	_ MatchArm    = &baseMatchArm{}
)

type NumberLiteral struct {
	baseExpression
	Value string
}

type ConstantReference struct {
	baseExpression
	Name string
}

type BinaryExpressionOp string

const (
	BinaryExpressionOpHas      BinaryExpressionOp = "has"
	BinaryExpressionOpMultiply BinaryExpressionOp = "*"
	BinaryExpressionOpJoin     BinaryExpressionOp = "@"
)

type BinaryExpression struct {
	baseExpression
	Lhs Expression
	Op  BinaryExpressionOp
	Rhs Expression
}

type LocalReference struct {
	baseExpression
	Name string
}

type MemberExpression struct {
	baseExpression
	Target Expression
	Name   *Identifier
}

type IndexExpression struct {
	baseExpression
	Target Expression
	Value  Expression
}

type ArrayExpression struct {
	baseExpression
	Values []Expression
}

type SimpleMatchArm struct {
	baseMatchArm
	Test  Expression
	Value Expression
}

type DefaultMatchArm struct {
	baseMatchArm
	Value Expression
}

type MatchExpression struct {
	baseExpression
	Value Expression
	Arms  []MatchArm
}

type ReadExpression struct {
	baseExpression
	Type Type
}

type UnimplementedExpression struct {
	baseExpression
}

type Identifier struct {
	baseNode
	Name string
}

type SizeStatement struct {
	baseStatement
	Value Expression
}

type JoinStatement struct {
	baseStatement
	Lhs *LocalReference
	Rhs *LocalReference
}

type StructureMember struct {
	baseStatement
	Name  *Identifier
	Type  Type
	Value Expression
}

type StructureField struct {
	baseStatement
	Name  *Identifier
	Type  Type
	Value Expression
}

type OffsetStatement struct {
	baseStatement
	ChangeBase bool
	Amount     Expression
	Body       []Statement
}

type TypeReference struct {
	baseType
	Name string
}

type InterfaceType struct {
	baseType
}

type StructureType struct {
	baseType
	Parent Type
	Body   []Statement
}

type BitfieldType struct {
	baseType
	Parent  Type
	Members []string
}

type ArrayType struct {
	baseType
	Count  Expression
	Member Type
}

type ConstantDeclaration struct {
	baseDeclaration
	Name  *Identifier
	Value Expression
}

type TypeDeclaration struct {
	baseDeclaration
	Name *Identifier
	Type Type
}

type RootDeclaration struct {
	baseDeclaration
	Type Type
}

type File struct {
	Declarations []Declaration
}

// Tokens

type token interface {
	tagToken()
}

type tokenComment string

// tagToken implements token.
func (tokenComment) tagToken() { panic("unimplemented") }

type tokenNewline string

// tagToken implements token.
func (tokenNewline) tagToken() { panic("unimplemented") }

type tokenWhitespace string

// tagToken implements token.
func (tokenWhitespace) tagToken() { panic("unimplemented") }

type tokenError struct{ e error }

// tagToken implements token.
func (*tokenError) tagToken() { panic("unimplemented") }

type tokenIdentifier string

// tagToken implements token.
func (tokenIdentifier) tagToken() { panic("unimplemented") }

type tokenLocalReference string

// tagToken implements token.
func (tokenLocalReference) tagToken() { panic("unimplemented") }

type tokenKeyword string

const (
	tokenKeywordConst         tokenKeyword = "const"
	tokenKeywordType          tokenKeyword = "type"
	tokenKeywordInterface     tokenKeyword = "interface"
	tokenKeywordStruct        tokenKeyword = "struct"
	tokenKeywordBitfield      tokenKeyword = "bitfield"
	tokenKeywordEnum          tokenKeyword = "enum"
	tokenKeywordSize          tokenKeyword = "size"
	tokenKeywordField         tokenKeyword = "field"
	tokenKeywordOffset        tokenKeyword = "offset"
	tokenKeywordOffsetBase    tokenKeyword = "offset_base"
	tokenKeywordMatch         tokenKeyword = "match"
	tokenKeywordRead          tokenKeyword = "read"
	tokenKeywordUnimplemented tokenKeyword = "unimplemented"
	tokenKeywordRoot          tokenKeyword = "root"
	tokenKeywordEmpty         tokenKeyword = "_"
)

// tagToken implements token.
func (tokenKeyword) tagToken() { panic("unimplemented") }

type tokenSpecial string

const (
	tokenSpecialEqual              tokenSpecial = "="
	tokenSpecialLambda             tokenSpecial = "=>"
	tokenSpecialPropertyAccess     tokenSpecial = "."
	tokenSpecialComma              tokenSpecial = ","
	tokenSpecialOpenCurlyBracket   tokenSpecial = "{"
	tokenSpecialCloseCurlyBracket  tokenSpecial = "}"
	tokenSpecialOpenAngleBracket   tokenSpecial = "<"
	tokenSpecialCloseAngleBracket  tokenSpecial = ">"
	tokenSpecialOpenSquareBracket  tokenSpecial = "["
	tokenSpecialCloseSquareBracket tokenSpecial = "]"
	tokenSpecialOpenRoundBracket   tokenSpecial = "("
	tokenSpecialCloseRoundBracket  tokenSpecial = ")"
	tokenSpecialMultiply           tokenSpecial = "*"
	tokenSpecialJoin               tokenSpecial = "@"
)

// tagToken implements token.
func (tokenSpecial) tagToken() { panic("unimplemented") }

type tokenNumber string

// tagToken implements token.
func (tokenNumber) tagToken() { panic("unimplemented") }

var (
	_ token = tokenComment("")
	_ token = tokenNewline("")
	_ token = tokenWhitespace("")
	_ token = tokenIdentifier("")
	_ token = tokenLocalReference("")
	_ token = tokenKeyword("")
	_ token = tokenSpecial("")
	_ token = tokenNumber("")
	_ token = &tokenError{}
)

type parser struct {
	reader     *bufio.Reader
	tokenStack []token
}

// Lexer

func validIdentifierChar(chr rune) bool {
	if chr >= 'a' && chr <= 'z' {
		return true
	} else if chr >= 'A' && chr <= 'Z' {
		return true
	} else if chr >= '0' && chr <= '9' {
		return true
	} else if chr == '_' {
		return true
	} else {
		return false
	}
}

func validNumberChar(chr rune) bool {
	if chr >= '0' && chr <= '9' {
		return true
	} else {
		return false
	}
}

func validWhitespaceChar(chr rune) bool {
	if chr == ' ' {
		return true
	} else if chr == '\t' {
		return true
	} else if chr == '\n' {
		return true
	} else {
		return false
	}
}

func (p *parser) nextComment() token {
	ret := ""

	for {
		chr, _, err := p.reader.ReadRune()
		if err == io.EOF {
			break
		} else if err != nil {
			return &tokenError{e: err}
		}

		if chr == '\n' {
			break
		} else {
			ret += string(chr)
		}
	}

	return tokenComment(ret)
}

func (p *parser) nextWhitespace() token {
	ret := ""

	for {
		chr, _, err := p.reader.ReadRune()
		if err == io.EOF {
			break
		} else if err != nil {
			return &tokenError{e: err}
		}

		if validWhitespaceChar(chr) {
			ret += string(chr)
		} else {
			p.reader.UnreadRune()
			break
		}
	}

	return tokenWhitespace(ret)
}

func (p *parser) nextIdentifierOrKeyword() token {
	ret := ""

	for {
		chr, _, err := p.reader.ReadRune()
		if err == io.EOF {
			break
		} else if err != nil {
			return &tokenError{e: err}
		}

		if validIdentifierChar(chr) {
			ret += string(chr)
		} else {
			p.reader.UnreadRune()
			break
		}
	}

	if ret == string(tokenKeywordConst) {
		return tokenKeywordConst
	} else if ret == string(tokenKeywordType) {
		return tokenKeywordType
	} else if ret == string(tokenKeywordInterface) {
		return tokenKeywordInterface
	} else if ret == string(tokenKeywordStruct) {
		return tokenKeywordStruct
	} else if ret == string(tokenKeywordBitfield) {
		return tokenKeywordBitfield
	} else if ret == string(tokenKeywordSize) {
		return tokenKeywordSize
	} else if ret == string(tokenKeywordField) {
		return tokenKeywordField
	} else if ret == string(tokenKeywordOffset) {
		return tokenKeywordOffset
	} else if ret == string(tokenKeywordOffsetBase) {
		return tokenKeywordOffsetBase
	} else if ret == string(tokenKeywordMatch) {
		return tokenKeywordMatch
	} else if ret == string(tokenKeywordRead) {
		return tokenKeywordRead
	} else if ret == string(tokenKeywordUnimplemented) {
		return tokenKeywordUnimplemented
	} else if ret == string(tokenKeywordRoot) {
		return tokenKeywordRoot
	} else if ret == string(tokenKeywordEmpty) {
		return tokenKeywordEmpty
	} else {
		return tokenIdentifier(ret)
	}
}

func (p *parser) nextNumber() token {
	ret := ""

	for {
		chr, _, err := p.reader.ReadRune()
		if err == io.EOF {
			break
		} else if err != nil {
			return &tokenError{e: err}
		}

		if validNumberChar(chr) || (chr >= 'A' && chr <= 'F') || (chr >= 'a' && chr <= 'f') {
			ret += string(chr)
		} else if chr == 'x' {
			ret += "x"
		} else if chr == '.' {
			ret += "."
		} else if chr == '_' {
			continue
		} else {
			p.reader.UnreadRune()
			break
		}
	}

	return tokenNumber(ret)
}

func (p *parser) pushToken(tk token) {
	p.tokenStack = append(p.tokenStack, tk)
}

func (p *parser) next() token {
	if len(p.tokenStack) > 0 {
		tk := p.tokenStack[0]
		p.tokenStack = p.tokenStack[1:]
		return tk
	}

	chr, _, err := p.reader.ReadRune()
	if err != nil {
		return &tokenError{e: err}
	}

	switch true {
	case chr == '/':
		next, _, err := p.reader.ReadRune()
		if err != nil {
			return &tokenError{e: err}
		}

		switch true {
		case next == '/':
			return p.nextComment()
		default:
			return &tokenError{e: fmt.Errorf("invalid character [%c%c]", chr, next)}
		}
	case validNumberChar(chr):
		p.reader.UnreadRune()
		return p.nextNumber()
	case validIdentifierChar(chr):
		p.reader.UnreadRune()
		return p.nextIdentifierOrKeyword()
	case chr == '\n':
		return tokenNewline(chr)
	case chr == ' ' || chr == '\t':
		p.reader.UnreadRune()
		return p.nextWhitespace()
	case chr == '$':
		id := p.nextIdentifierOrKeyword()

		if ident, ok := id.(tokenIdentifier); ok {
			return tokenLocalReference(ident)
		} else if e, ok := id.(*tokenError); ok {
			return e
		} else {
			return &tokenError{e: fmt.Errorf("invalid local reference: %+v", id)}
		}
	case chr == '=':
		next, _, err := p.reader.ReadRune()
		if err != nil {
			return &tokenError{e: err}
		}

		switch true {
		case next == '>':
			return tokenSpecialLambda
		default:
			p.reader.UnreadRune()
			return tokenSpecialEqual
		}
	case chr == '.':
		return tokenSpecialPropertyAccess
	case chr == ',':
		return tokenSpecialComma
	case chr == '*':
		return tokenSpecialMultiply
	case chr == '{':
		return tokenSpecialOpenCurlyBracket
	case chr == '}':
		return tokenSpecialCloseCurlyBracket
	case chr == '<':
		return tokenSpecialOpenAngleBracket
	case chr == '>':
		return tokenSpecialCloseAngleBracket
	case chr == '[':
		return tokenSpecialOpenSquareBracket
	case chr == ']':
		return tokenSpecialCloseSquareBracket
	case chr == '(':
		return tokenSpecialOpenRoundBracket
	case chr == ')':
		return tokenSpecialCloseRoundBracket
	case chr == '@':
		return tokenSpecialJoin
	default:
		return &tokenError{e: fmt.Errorf("invalid character [%c]", chr)}
	}
}

func (p *parser) nextSignificant() token {
	for {
		tk := p.next()
		if e, ok := tk.(*tokenError); ok {
			return e
		}

		slog.Debug("", "token", fmt.Sprintf("%T", tk), "value", tk)

		switch tk.(type) {
		case tokenComment:
			continue
		case tokenNewline:
			continue
		case tokenWhitespace:
			continue
		default:
			return tk
		}
	}
}

func (p *parser) matchSpecial(sp tokenSpecial) error {
	tk := p.nextSignificant()

	if tk == sp {
		return nil
	} else {
		return fmt.Errorf("invalid syntax: expected [%s] got [%s]", sp, tk)
	}
}

// Parser

func (p *parser) parseIdentifier() (*Identifier, error) {
	tk := p.nextSignificant()
	if e, ok := tk.(*tokenError); ok {
		return nil, e.e
	}

	if id, ok := tk.(tokenIdentifier); ok {
		return &Identifier{Name: string(id)}, nil
	} else {
		return nil, fmt.Errorf("syntax error: expected identifier")
	}
}

func (p *parser) parseBinaryExpressionOp() (BinaryExpressionOp, error) {
	op := p.nextSignificant()
	if e, ok := op.(*tokenError); ok {
		return "", e.e
	}

	switch op {
	case tokenIdentifier("has"):
		return BinaryExpressionOpHas, nil
	case tokenSpecialMultiply:
		return BinaryExpressionOpMultiply, nil
	case tokenSpecialJoin:
		return BinaryExpressionOpJoin, nil
	default:
		return "", fmt.Errorf("invalid binary expression op: %T %+v", op, op)
	}
}

func (p *parser) parseBinaryExpression() (*BinaryExpression, error) {
	lhs, err := p.parseExpression()
	if err != nil {
		return nil, err
	}

	op, err := p.parseBinaryExpressionOp()
	if err != nil {
		return nil, err
	}

	rhs, err := p.parseExpression()
	if err != nil {
		return nil, err
	}

	if err := p.matchSpecial(tokenSpecialCloseRoundBracket); err != nil {
		return nil, err
	}

	return &BinaryExpression{
		Lhs: lhs,
		Op:  op,
		Rhs: rhs,
	}, nil
}

func (p *parser) parseArrayExpression() (*ArrayExpression, error) {
	ret := &ArrayExpression{}

	for {
		expr, err := p.parseExpression()
		if err != nil {
			return nil, err
		}

		ret.Values = append(ret.Values, expr)

		next := p.nextSignificant()
		if e, ok := next.(*tokenError); ok {
			return nil, e.e
		}

		if next == tokenSpecialComma {
			continue
		} else if next == tokenSpecialCloseSquareBracket {
			break
		} else {
			return nil, fmt.Errorf("invalid array expression syntax: %T %+v", next, next)
		}
	}

	return ret, nil
}

func (p *parser) parseMatchArm() (MatchArm, error) {
	lhsTk := p.nextSignificant()
	if e, ok := lhsTk.(*tokenError); ok {
		return nil, e.e
	}

	if lhsTk == tokenSpecialCloseCurlyBracket {
		return nil, nil
	} else if lhsTk == tokenKeywordEmpty {
		if err := p.matchSpecial(tokenSpecialLambda); err != nil {
			return nil, err
		}

		rhs, err := p.parseExpression()
		if err != nil {
			return nil, err
		}

		return &DefaultMatchArm{
			Value: rhs,
		}, nil
	} else {
		p.pushToken(lhsTk)

		lhs, err := p.parseExpression()
		if err != nil {
			return nil, err
		}

		if err := p.matchSpecial(tokenSpecialLambda); err != nil {
			return nil, err
		}

		rhs, err := p.parseExpression()
		if err != nil {
			return nil, err
		}

		return &SimpleMatchArm{
			Test:  lhs,
			Value: rhs,
		}, nil
	}
}

func (p *parser) parseMatchExpression() (*MatchExpression, error) {
	value, err := p.parseExpression()
	if err != nil {
		return nil, err
	}

	if err := p.matchSpecial(tokenSpecialOpenCurlyBracket); err != nil {
		return nil, err
	}

	ret := &MatchExpression{
		Value: value,
	}

	for {
		arm, err := p.parseMatchArm()
		if err != nil {
			return nil, err
		}
		if arm == nil {
			break
		}

		ret.Arms = append(ret.Arms, arm)
	}

	return ret, nil

}

func (p *parser) parseReadExpression() (*ReadExpression, error) {
	if err := p.matchSpecial(tokenSpecialOpenRoundBracket); err != nil {
		return nil, err
	}

	typ, err := p.parseType()
	if err != nil {
		return nil, err
	}

	if err := p.matchSpecial(tokenSpecialCloseRoundBracket); err != nil {
		return nil, err
	}

	return &ReadExpression{Type: typ}, nil
}

func (p *parser) parseExpression() (Expression, error) {
	tk := p.nextSignificant()
	if e, ok := tk.(*tokenError); ok {
		return nil, e.e
	}

	var expr Expression

	switch tk := tk.(type) {
	case tokenNumber:
		expr = &NumberLiteral{Value: string(tk)}
	case tokenIdentifier:
		expr = &ConstantReference{Name: string(tk)}
	case tokenLocalReference:
		expr = &LocalReference{Name: string(tk)}
	case tokenSpecial:
		switch tk {
		case tokenSpecialOpenRoundBracket:
			e, err := p.parseBinaryExpression()
			if err != nil {
				return nil, err
			}

			expr = e
		case tokenSpecialOpenSquareBracket:
			e, err := p.parseArrayExpression()
			if err != nil {
				return nil, err
			}

			expr = e
		default:
			return nil, fmt.Errorf("invalid expression: %T %+v", tk, tk)
		}
	case tokenKeyword:
		switch tk {
		case tokenKeywordMatch:
			e, err := p.parseMatchExpression()
			if err != nil {
				return nil, err
			}

			expr = e
		case tokenKeywordRead:
			e, err := p.parseReadExpression()
			if err != nil {
				return nil, err
			}

			expr = e
		case tokenKeywordUnimplemented:
			expr = &UnimplementedExpression{}
		default:
			return nil, fmt.Errorf("invalid expression: %T %+v", tk, tk)
		}
	default:
		return nil, fmt.Errorf("invalid expression: %T %+v", tk, tk)
	}

outer:
	for {
		next := p.next()
		if e, ok := next.(*tokenError); ok {
			return nil, e.e
		}

		switch next := next.(type) {
		case tokenSpecial:
			switch next {
			case tokenSpecialPropertyAccess:
				name, err := p.parseIdentifier()
				if err != nil {
					return nil, err
				}

				expr = &MemberExpression{
					Target: expr,
					Name:   name,
				}

				continue outer
			case tokenSpecialOpenSquareBracket:
				value, err := p.parseExpression()
				if err != nil {
					return nil, err
				}

				if err := p.matchSpecial(tokenSpecialCloseSquareBracket); err != nil {
					return nil, err
				}

				expr = &IndexExpression{
					Target: expr,
					Value:  value,
				}

				continue outer
			}
		}

		p.pushToken(next)
		return expr, nil
	}
}

func (p *parser) parseSizeStatement() (*SizeStatement, error) {
	if err := p.matchSpecial(tokenSpecialEqual); err != nil {
		return nil, err
	}

	value, err := p.parseExpression()
	if err != nil {
		return nil, err
	}

	return &SizeStatement{
		Value: value,
	}, nil
}

func (p *parser) parseStructMember(name *Identifier) (*StructureMember, error) {
	next := p.nextSignificant()

	ret := &StructureMember{Name: name}

	if next != tokenSpecialEqual {
		p.pushToken(next)

		typ, err := p.parseType()
		if err != nil {
			return nil, err
		}

		ret.Type = typ
	}

	next = p.nextSignificant()

	if next == tokenSpecialEqual {
		value, err := p.parseExpression()
		if err != nil {
			return nil, err
		}

		ret.Value = value
	} else {
		p.pushToken(next)
	}

	return ret, nil
}

func (p *parser) parseStructField() (*StructureField, error) {
	name, err := p.parseIdentifier()
	if err != nil {
		return nil, err
	}

	member, err := p.parseStructMember(name)
	if err != nil {
		return nil, err
	}

	return &StructureField{
		Name:  member.Name,
		Type:  member.Type,
		Value: member.Value,
	}, nil
}

func (p *parser) parseOffsetStatement(changeBase bool) (*OffsetStatement, error) {
	amount, err := p.parseExpression()
	if err != nil {
		return nil, err
	}

	if err := p.matchSpecial(tokenSpecialOpenCurlyBracket); err != nil {
		return nil, err
	}

	ret := &OffsetStatement{
		ChangeBase: changeBase,
		Amount:     amount,
	}

	for {
		stmt, err := p.parseStatement()
		if err != nil {
			return nil, err
		}
		if stmt == nil {
			break
		}

		ret.Body = append(ret.Body, stmt)
	}

	return ret, nil
}

func (p *parser) parseStatement() (Statement, error) {
	tk := p.nextSignificant()
	if e, ok := tk.(*tokenError); ok {
		return nil, e.e
	}

	switch stmt := tk.(type) {
	case tokenSpecial:
		switch stmt {
		case tokenSpecialCloseCurlyBracket:
			return nil, nil
		default:
			return nil, fmt.Errorf("invalid statement: %T %+v", stmt, stmt)
		}
	case tokenKeyword:
		switch stmt {
		case tokenKeywordSize:
			return p.parseSizeStatement()
		case tokenKeywordField:
			return p.parseStructField()
		case tokenKeywordEmpty:
			return p.parseStructMember(nil)
		case tokenKeywordOffsetBase:
			return p.parseOffsetStatement(true)
		default:
			return nil, fmt.Errorf("invalid statement: %T %+v", stmt, stmt)
		}
	case tokenIdentifier:
		return p.parseStructMember(&Identifier{Name: string(stmt)})
	default:
		return nil, fmt.Errorf("invalid statement: %T %+v", stmt, stmt)
	}
}

func (p *parser) parseInterfaceType() (*InterfaceType, error) {
	if err := p.matchSpecial(tokenSpecialOpenCurlyBracket); err != nil {
		return nil, err
	}

	if err := p.matchSpecial(tokenSpecialCloseCurlyBracket); err != nil {
		return nil, err
	}

	return &InterfaceType{}, nil
}

func (p *parser) parseStructureType() (*StructureType, error) {
	ret := &StructureType{}

outer:
	for {
		next := p.nextSignificant()
		if e, ok := next.(*tokenError); ok {
			return nil, e.e
		}

		switch next := next.(type) {
		case tokenSpecial:
			switch next {
			case tokenSpecialOpenAngleBracket:
				parent, err := p.parseType()
				if err != nil {
					return nil, err
				}

				if err := p.matchSpecial(tokenSpecialCloseAngleBracket); err != nil {
					return nil, err
				}

				ret.Parent = parent
			case tokenSpecialOpenCurlyBracket:
				break outer
			default:
				return nil, fmt.Errorf("invalid structure: %T %+v", next, next)
			}
		default:
			return nil, fmt.Errorf("invalid structure: %T %+v", next, next)
		}
	}

	for {
		stmt, err := p.parseStatement()
		if err != nil {
			return nil, err
		}
		if stmt == nil {
			break
		}

		ret.Body = append(ret.Body, stmt)
	}

	return ret, nil
}

func (p *parser) parseArrayType() (*ArrayType, error) {
	ret := &ArrayType{}

	next := p.nextSignificant()

	if next != tokenSpecialCloseSquareBracket {
		p.pushToken(next)

		count, err := p.parseExpression()
		if err != nil {
			return nil, err
		}

		if err := p.matchSpecial(tokenSpecialCloseSquareBracket); err != nil {
			return nil, err
		}

		ret.Count = count
	}

	member, err := p.parseType()
	if err != nil {
		return nil, err
	}

	ret.Member = member

	return ret, nil
}

func (p *parser) parseBitfieldType() (*BitfieldType, error) {
	ret := &BitfieldType{}

	if err := p.matchSpecial(tokenSpecialOpenAngleBracket); err != nil {
		return nil, err
	}

	parent, err := p.parseType()
	if err != nil {
		return nil, err
	}

	ret.Parent = parent

	if err := p.matchSpecial(tokenSpecialCloseAngleBracket); err != nil {
		return nil, err
	}

	if err := p.matchSpecial(tokenSpecialOpenCurlyBracket); err != nil {
		return nil, err
	}

outer:
	for {
		body := p.nextSignificant()
		if e, ok := body.(*tokenError); ok {
			return nil, e.e
		}

		switch body := body.(type) {
		case tokenIdentifier:
			ret.Members = append(ret.Members, string(body))
		case tokenKeyword:
			if body == tokenKeywordEmpty {
				ret.Members = append(ret.Members, "")
			} else {
				return nil, fmt.Errorf("invalid bitfield member: %T %+v", body, body)
			}
		case tokenSpecial:
			if body == tokenSpecialCloseCurlyBracket {
				break outer
			} else {
				return nil, fmt.Errorf("invalid bitfield member: %T %+v", body, body)
			}
		default:
			return nil, fmt.Errorf("invalid bitfield member: %T %+v", body, body)
		}
	}

	return ret, nil
}

func (p *parser) parseType() (Type, error) {
	tk := p.nextSignificant()
	if e, ok := tk.(*tokenError); ok {
		return nil, e.e
	}

	switch typ := tk.(type) {
	case tokenSpecial:
		switch typ {
		case tokenSpecialOpenSquareBracket:
			return p.parseArrayType()
		default:
			return nil, fmt.Errorf("invalid type: %T %+v", typ, typ)
		}
	case tokenIdentifier:
		return &TypeReference{Name: string(typ)}, nil
	case tokenKeyword:
		switch typ {
		case tokenKeywordInterface:
			return p.parseInterfaceType()
		case tokenKeywordStruct:
			return p.parseStructureType()
		case tokenKeywordBitfield:
			return p.parseBitfieldType()
		default:
			return nil, fmt.Errorf("invalid type: %T %+v", typ, typ)
		}
	default:
		return nil, fmt.Errorf("invalid type: %T %+v", typ, typ)
	}
}

func (p *parser) parseConstantDeclaration() (*ConstantDeclaration, error) {
	name, err := p.parseIdentifier()
	if err != nil {
		return nil, err
	}

	if err := p.matchSpecial(tokenSpecialEqual); err != nil {
		return nil, err
	}

	value, err := p.parseExpression()
	if err != nil {
		return nil, err
	}

	return &ConstantDeclaration{
		Name:  name,
		Value: value,
	}, nil
}

func (p *parser) parseTypeDeclaration() (*TypeDeclaration, error) {
	name, err := p.parseIdentifier()
	if err != nil {
		return nil, err
	}

	typ, err := p.parseType()
	if err != nil {
		return nil, err
	}

	return &TypeDeclaration{
		Name: name,
		Type: typ,
	}, nil
}

func (p *parser) parseRootDeclaration() (*RootDeclaration, error) {
	if err := p.matchSpecial(tokenSpecialEqual); err != nil {
		return nil, err
	}

	typ, err := p.parseType()
	if err != nil {
		return nil, err
	}

	return &RootDeclaration{
		Type: typ,
	}, nil
}

func (p *parser) parseDeclaration() (Declaration, error) {
	tk := p.nextSignificant()
	if e, ok := tk.(*tokenError); ok {
		if e.e == io.EOF {
			return nil, nil
		} else {
			return nil, e.e
		}
	}

	switch decl := tk.(type) {
	case tokenKeyword:
		switch decl {
		case tokenKeywordConst:
			return p.parseConstantDeclaration()
		case tokenKeywordType:
			return p.parseTypeDeclaration()
		case tokenKeywordRoot:
			return p.parseRootDeclaration()
		default:
			return nil, fmt.Errorf("invalid declaration: %T %+v", decl, decl)
		}
	default:
		return nil, fmt.Errorf("invalid declaration: %T %+v", decl, decl)
	}
}

func Parse(r io.Reader) (*File, error) {
	parser := &parser{reader: bufio.NewReader(r)}

	f := &File{}

	for {
		decl, err := parser.parseDeclaration()
		if err != nil {
			return nil, err
		}
		if decl == nil {
			break
		}

		f.Declarations = append(f.Declarations, decl)
	}

	return f, nil
}

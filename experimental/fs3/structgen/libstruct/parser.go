package libstruct

import (
	"bufio"
	"fmt"
	"io"
)

// Systems Immediate Language 4 (sysIL4) is a simple language that is used to define data structures.
type sysIL4Parser struct {
	in     *bufio.Reader
	tokens []token
}

func (p *sysIL4Parser) parseIdentifier() (string, error) {
	tk, err := p.nextSignificantToken()
	if err != nil {
		return "", err
	}

	if _, ok := tk.(identifierToken); !ok {
		return "", fmt.Errorf("expected identifier, got %T %+v", tk, tk)
	}

	return string(tk.(identifierToken)), nil
}

func (p *sysIL4Parser) parseArrayTypeInstance() (TypeInstance, error) {
	ret := &TypeInstanceArray{}

	size, err := p.parseExpression()
	if err != nil {
		return nil, err
	}

	if err := p.expectToken(specialCloseSquareBracket); err != nil {
		return nil, err
	}

	elementType, err := p.parseTypeInstance()
	if err != nil {
		return nil, err
	}

	ret.Size = size
	ret.ElementType = elementType

	return ret, nil
}

func (p *sysIL4Parser) parseTypeInstance() (TypeInstance, error) {
	tk, err := p.nextSignificantToken()
	if err != nil {
		return nil, err
	}

	switch tk := tk.(type) {
	case identifierToken:
		switch tk {
		case identifierToken(TypeInstanceBuiltinUint8):
			return TypeInstanceBuiltinUint8, nil
		case identifierToken(TypeInstanceBuiltinUint16LE):
			return TypeInstanceBuiltinUint16LE, nil
		case identifierToken(TypeInstanceBuiltinUint32LE):
			return TypeInstanceBuiltinUint32LE, nil
		case identifierToken(TypeInstanceBuiltinUint64LE):
			return TypeInstanceBuiltinUint64LE, nil
		case identifierToken(TypeInstanceBuiltinUint16BE):
			return TypeInstanceBuiltinUint16BE, nil
		case identifierToken(TypeInstanceBuiltinUint32BE):
			return TypeInstanceBuiltinUint32BE, nil
		case identifierToken(TypeInstanceBuiltinUint64BE):
			return TypeInstanceBuiltinUint64BE, nil
		case identifierToken(TypeInstanceBuiltinInt8):
			return TypeInstanceBuiltinInt8, nil
		case identifierToken(TypeInstanceBuiltinInt16LE):
			return TypeInstanceBuiltinInt16LE, nil
		case identifierToken(TypeInstanceBuiltinInt32LE):
			return TypeInstanceBuiltinInt32LE, nil
		case identifierToken(TypeInstanceBuiltinInt64LE):
			return TypeInstanceBuiltinInt64LE, nil
		case identifierToken(TypeInstanceBuiltinInt16BE):
			return TypeInstanceBuiltinInt16BE, nil
		case identifierToken(TypeInstanceBuiltinInt32BE):
			return TypeInstanceBuiltinInt32BE, nil
		case identifierToken(TypeInstanceBuiltinInt64BE):
			return TypeInstanceBuiltinInt64BE, nil
		case identifierToken(TypeInstanceBuiltinChar):
			return TypeInstanceBuiltinChar, nil
		default:
			return TypeInstanceReference(tk), nil
		}
	case specialToken:
		switch tk {
		case specialOpenSquareBracket:
			return p.parseArrayTypeInstance()
		default:
			return nil, fmt.Errorf("unexpected special token: %q", tk)
		}
	default:
		return nil, fmt.Errorf("expected identifier, got %T", tk)
	}
}

func (p *sysIL4Parser) parseExpressionFragment() (Expression, error) {
	tk, err := p.nextSignificantToken()
	if err != nil {
		return nil, err
	}

	switch tk := tk.(type) {
	case numberLiteralToken:
		return NumberLiteralExpression(tk), nil
	case referenceToken:
		return VariableReferenceExpression(tk), nil
	default:
		return nil, fmt.Errorf("unimplemented, got %T", tk)
	}
}

func (p *sysIL4Parser) parseExpression() (Expression, error) {
	frag, err := p.parseExpressionFragment()
	if err != nil {
		return nil, err
	}

	ret := frag

outer:
	for {
		// peak ahead to see if we have an operator
		tk, err := p.nextSignificantToken()
		if err != nil {
			return nil, err
		}

		switch tk {
		case specialMinus:
			second, err := p.parseExpressionFragment()
			if err != nil {
				return nil, err
			}

			ret = &BinaryExpression{
				Op:  BinaryOpMinus,
				Lhs: ret,
				Rhs: second,
			}
		case specialPlus:
			second, err := p.parseExpressionFragment()
			if err != nil {
				return nil, err
			}

			ret = &BinaryExpression{
				Op:  BinaryOpPlus,
				Lhs: ret,
				Rhs: second,
			}
		default:
			p.unreadToken(tk)
			break outer
		}
	}

	return ret, nil
}

func (p *sysIL4Parser) parseEnumMember() (*EnumMember, error) {
	ret := &EnumMember{}

	name, err := p.parseIdentifier()
	if err != nil {
		return nil, fmt.Errorf("failed to parse identifier for enumMember: %w", err)
	}
	ret.Name = name

	if err := p.expectToken(specialEquals); err != nil {
		return nil, fmt.Errorf("failed to parse equals for enumMember: %w", err)
	}

	value, err := p.parseExpression()
	if err != nil {
		return nil, fmt.Errorf("failed to parse expression for enumMember: %w", err)
	}
	ret.Value = value

	return ret, nil
}

func (p *sysIL4Parser) parseEnumDeclaration() (*EnumDeclaration, error) {
	ret := &EnumDeclaration{}

	if err := p.expectToken(specialOpenBlock); err != nil {
		return nil, err
	}

	for {
		next, err := p.nextSignificantToken()
		if err != nil {
			return nil, err
		}

		if next == specialCloseBlock {
			break
		}

		if next == keywordType {
			if ret.ValueType != nil {
				return nil, fmt.Errorf("duplicate type declaration")
			}

			instance, err := p.parseTypeInstance()
			if err != nil {
				return nil, err
			}

			ret.ValueType = instance

			continue
		}

		p.unreadToken(next)

		member, err := p.parseEnumMember()
		if err != nil {
			return nil, err
		}

		ret.Members = append(ret.Members, member)
	}

	return ret, nil
}

func (p *sysIL4Parser) parseStructOrUnionMember() (StructMember, error) {
	ret := &StructOrUnionMember{}

	tk, err := p.nextSignificantToken()
	if err != nil {
		return nil, err
	}

	if tk == keywordSize {
		if err := p.expectToken(specialEquals); err != nil {
			return nil, fmt.Errorf("expected equals after size")
		}

		expr, err := p.parseExpression()
		if err != nil {
			return nil, fmt.Errorf("failed to parse expression for size: %w", err)
		}

		return &StructSizeMember{Size: expr}, nil
	} else {
		p.unreadToken(tk)
	}

	name, err := p.parseIdentifier()
	if err != nil {
		return nil, err
	}
	ret.Name = name

	typeInstance, err := p.parseTypeInstance()
	if err != nil {
		return nil, err
	}
	ret.ValueType = typeInstance

	return ret, nil
}

func (p *sysIL4Parser) parseStructConstMember() (*StructConstMember, error) {
	ret := &StructConstMember{}

	name, err := p.parseIdentifier()
	if err != nil {
		return nil, err
	}
	ret.Name = name

	if err := p.expectToken(specialEquals); err != nil {
		return nil, err
	}

	value, err := p.parseExpression()
	if err != nil {
		return nil, err
	}
	ret.Value = value

	return ret, nil
}

func (p *sysIL4Parser) parseStructMember() (StructMember, error) {
	tk, err := p.nextSignificantToken()
	if err != nil {
		return nil, err
	}

	switch tk {
	case keywordConst:
		return p.parseStructConstMember()
	default:
		p.unreadToken(tk)
		return p.parseStructOrUnionMember()
	}
}

func (p *sysIL4Parser) parseStructDeclaration(dynamic bool) (*StructDeclaration, error) {
	ret := &StructDeclaration{Dynamic: dynamic}

	if err := p.expectToken(specialOpenBlock); err != nil {
		return nil, err
	}

	for {
		next, err := p.nextSignificantToken()
		if err != nil {
			return nil, err
		}

		if next == specialCloseBlock {
			break
		}

		p.unreadToken(next)

		member, err := p.parseStructMember()
		if err != nil {
			return nil, err
		}

		ret.Members = append(ret.Members, member)
	}

	return ret, nil
}

func (p *sysIL4Parser) parseUnionDeclaration() (*UnionDeclaration, error) {
	ret := &UnionDeclaration{}

	if err := p.expectToken(specialOpenBlock); err != nil {
		return nil, err
	}

	for {
		next, err := p.nextSignificantToken()
		if err != nil {
			return nil, err
		}

		if next == specialCloseBlock {
			break
		}

		p.unreadToken(next)

		member, err := p.parseStructOrUnionMember()
		if err != nil {
			return nil, err
		}

		ret.Members = append(ret.Members, member)
	}

	return ret, nil
}

func (p *sysIL4Parser) parseBitsetDeclaration() (*BitsetDeclaration, error) {
	ret := &BitsetDeclaration{}

	if err := p.expectToken(specialOpenBlock); err != nil {
		return nil, err
	}

	for {
		next, err := p.nextSignificantToken()
		if err != nil {
			return nil, err
		}

		if next == keywordType {
			valueType, err := p.parseTypeInstance()
			if err != nil {
				return nil, err
			}

			ret.ValueType = valueType

			continue
		}

		if next == specialCloseBlock {
			break
		}

		if _, ok := next.(identifierToken); !ok {
			return nil, fmt.Errorf("expected identifier, got %T", next)
		}

		ret.Members = append(ret.Members, string(next.(identifierToken)))
	}

	return ret, nil
}

func (p *sysIL4Parser) parseDeclaredType() (TypeInstance, error) {
	tk, err := p.nextKeyword()
	if err != nil {
		return nil, err
	}

	switch tk {
	case keywordEnum:
		return p.parseEnumDeclaration()
	case keywordStruct:
		return p.parseStructDeclaration(false)
	case keywordUnion:
		return p.parseUnionDeclaration()
	case keywordBitset:
		return p.parseBitsetDeclaration()
	case keywordDynamicStruct:
		return p.parseStructDeclaration(true)
	default:
		return nil, fmt.Errorf("unexpected keyword: %q", tk)
	}
}

func (p *sysIL4Parser) parseTypeDeclaration() (*TypeDeclaration, error) {
	name, err := p.parseIdentifier()
	if err != nil {
		return nil, err
	}

	innerType, err := p.parseDeclaredType()
	if err != nil {
		return nil, err
	}

	return &TypeDeclaration{
		Name:      name,
		InnerType: innerType,
	}, nil
}

func (p *sysIL4Parser) parseDeclaration() (Declaration, error) {
	tk, err := p.nextKeyword()
	if err != nil {
		return nil, err
	}

	switch tk {
	case keywordType:
		return p.parseTypeDeclaration()
	default:
		return nil, fmt.Errorf("unexpected keyword: %q", tk)
	}
}

func (p *sysIL4Parser) parse() (*File, error) {
	ret := &File{}

	for {
		decl, err := p.parseDeclaration()
		if err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}

		ret.Declarations = append(ret.Declarations, decl)
	}

	return ret, nil
}

func Parse(r io.Reader) (*File, error) {
	p := &sysIL4Parser{
		in: bufio.NewReader(r),
	}

	return p.parse()
}

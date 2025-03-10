package main

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
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

func (p *sysIL4Parser) parseArrayTypeInstance() (typeInstance, error) {
	ret := &typeInstanceArray{}

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

	ret.size = size
	ret.elementType = elementType

	return ret, nil
}

func (p *sysIL4Parser) parseTypeInstance() (typeInstance, error) {
	tk, err := p.nextSignificantToken()
	if err != nil {
		return nil, err
	}

	switch tk := tk.(type) {
	case identifierToken:
		switch tk {
		case "u8":
			return typeInstanceBuiltinUint8, nil
		default:
			return typeInstanceReference(tk), nil
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

func (p *sysIL4Parser) parseExpression() (expression, error) {
	tk, err := p.nextSignificantToken()
	if err != nil {
		return nil, err
	}

	var first expression

	switch tk := tk.(type) {
	case numberLiteralToken:
		first = numberLiteralExpression(tk)
	case referenceToken:
		first = variableReferenceExpression(tk)
	default:
		return nil, fmt.Errorf("unimplemented, got %T", tk)
	}

	// peak ahead to see if we have an operator
	tk, err = p.nextSignificantToken()
	if err != nil {
		return nil, err
	}

	switch tk {
	case specialMinus:
		second, err := p.parseExpression()
		if err != nil {
			return nil, err
		}

		return &binaryExpression{
			op:  binaryOpMinus,
			lhs: first,
			rhs: second,
		}, nil
	default:
		p.unreadToken(tk)
		return first, nil
	}
}

func (p *sysIL4Parser) parseEnumMember() (*enumMember, error) {
	ret := &enumMember{}

	name, err := p.parseIdentifier()
	if err != nil {
		return nil, fmt.Errorf("failed to parse identifier for enumMember: %w", err)
	}
	ret.name = name

	if err := p.expectToken(specialEquals); err != nil {
		return nil, fmt.Errorf("failed to parse equals for enumMember: %w", err)
	}

	value, err := p.parseExpression()
	if err != nil {
		return nil, fmt.Errorf("failed to parse expression for enumMember: %w", err)
	}
	ret.value = value

	return ret, nil
}

func (p *sysIL4Parser) parseEnumDeclaration() (*enumDeclaration, error) {
	ret := &enumDeclaration{}

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
			if ret.valueType != nil {
				return nil, fmt.Errorf("duplicate type declaration")
			}

			instance, err := p.parseTypeInstance()
			if err != nil {
				return nil, err
			}

			ret.valueType = instance

			continue
		}

		p.unreadToken(next)

		member, err := p.parseEnumMember()
		if err != nil {
			return nil, err
		}

		ret.members = append(ret.members, member)
	}

	return ret, nil
}

func (p *sysIL4Parser) parseStructOrUnionMember() (*structOrUnionMember, error) {
	ret := &structOrUnionMember{}

	name, err := p.parseIdentifier()
	if err != nil {
		return nil, err
	}
	ret.name = name

	typeInstance, err := p.parseTypeInstance()
	if err != nil {
		return nil, err
	}
	ret.valueType = typeInstance

	return ret, nil
}

func (p *sysIL4Parser) parseStructDeclaration(dynamic bool) (*structDeclaration, error) {
	ret := &structDeclaration{dynamic: dynamic}

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

		ret.members = append(ret.members, member)
	}

	return ret, nil
}

func (p *sysIL4Parser) parseUnionDeclaration() (*unionDeclaration, error) {
	ret := &unionDeclaration{}

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

		ret.members = append(ret.members, member)
	}

	return ret, nil
}

func (p *sysIL4Parser) parseBitsetDeclaration() (*bitsetDeclaration, error) {
	ret := &bitsetDeclaration{}

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

		if _, ok := next.(identifierToken); !ok {
			return nil, fmt.Errorf("expected identifier, got %T", next)
		}

		ret.members = append(ret.members, string(next.(identifierToken)))
	}

	return ret, nil
}

func (p *sysIL4Parser) parseDeclaredType() (declaredType, error) {
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

func (p *sysIL4Parser) parseTypeDeclaration() (*typeDeclaration, error) {
	name, err := p.parseIdentifier()
	if err != nil {
		return nil, err
	}

	innerType, err := p.parseDeclaredType()
	if err != nil {
		return nil, err
	}

	return &typeDeclaration{
		name:      name,
		innerType: innerType,
	}, nil
}

func (p *sysIL4Parser) parseDeclaration() (declaration, error) {
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

func (p *sysIL4Parser) parse() (*file, error) {
	ret := &file{}

	for {
		decl, err := p.parseDeclaration()
		if err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}

		slog.Info("parsed declaration", "decl", decl)

		ret.declarations = append(ret.declarations, decl)
	}

	return ret, nil
}

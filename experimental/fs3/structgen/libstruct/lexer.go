package libstruct

import "fmt"

type token interface {
	tagToken()
}

type keywordToken string

func (k keywordToken) tagToken() {}

const (
	keywordType          keywordToken = "type"
	keywordEnum          keywordToken = "enum"
	keywordStruct        keywordToken = "struct"
	keywordUnion         keywordToken = "union"
	keywordBitset        keywordToken = "bitset"
	keywordDynamicStruct keywordToken = "dyn_struct"
	keywordConst         keywordToken = "const"
	keywordSize          keywordToken = "size"
)

type identifierToken string

func (i identifierToken) tagToken() {}

type whitespaceToken string

func (w whitespaceToken) tagToken() {}

type specialToken string

func (s specialToken) tagToken() {}

const (
	specialOpenBlock          specialToken = "{"
	specialCloseBlock         specialToken = "}"
	specialOpenSquareBracket  specialToken = "["
	specialCloseSquareBracket specialToken = "]"
	specialEquals             specialToken = "="
	specialReference          specialToken = "$"
	specialMinus              specialToken = "-"
	specialPlus               specialToken = "+"
)

type numberLiteralToken string

func (n numberLiteralToken) tagToken() {}

type commentToken string

func (c commentToken) tagToken() {}

type referenceToken string

func (r referenceToken) tagToken() {}

var (
	_ token = keywordToken("")
	_ token = identifierToken("")
	_ token = whitespaceToken("")
	_ token = specialToken("")
	_ token = numberLiteralToken("")
	_ token = commentToken("")
	_ token = referenceToken("")
)

func isIdentifierChar(r rune, first bool) bool {
	switch {
	case r >= 'a' && r <= 'z':
		return true
	case r >= 'A' && r <= 'Z':
		return true
	case r >= '0' && r <= '9' && !first:
		return true
	case r == '_':
		return true
	default:
		return false
	}
}

func (p *sysIL4Parser) nextIdentifier(first rune) (token, error) {
	ret := string(first)

	for {
		r, _, err := p.in.ReadRune()
		if err != nil {
			return nil, err
		}

		if !isIdentifierChar(r, false) {
			_ = p.in.UnreadRune()
			break
		}

		ret += string(r)
	}

	switch ret {
	case string(keywordType):
		return keywordType, nil
	case string(keywordEnum):
		return keywordEnum, nil
	case string(keywordStruct):
		return keywordStruct, nil
	case string(keywordUnion):
		return keywordUnion, nil
	case string(keywordBitset):
		return keywordBitset, nil
	case string(keywordDynamicStruct):
		return keywordDynamicStruct, nil
	case string(keywordConst):
		return keywordConst, nil
	case string(keywordSize):
		return keywordSize, nil
	default:
		return identifierToken(ret), nil
	}
}

func isWhitespaceChar(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\r':
		return true
	default:
		return false
	}
}

func (p *sysIL4Parser) nextWhitespace(first rune) (token, error) {
	ret := string(first)

	for {
		r, _, err := p.in.ReadRune()
		if err != nil {
			return nil, err
		}

		if !isWhitespaceChar(r) {
			_ = p.in.UnreadRune()
			break
		}

		ret += string(r)
	}

	return whitespaceToken(ret), nil
}

func isNumberLiteralChar(r rune, first bool) bool {
	switch {
	case r >= '0' && r <= '9':
		return true
	case r == '.' && !first:
		return true
	default:
		return false
	}
}

func (p *sysIL4Parser) nextNumberLiteral(first rune) (token, error) {
	ret := string(first)

	for {
		r, _, err := p.in.ReadRune()
		if err != nil {
			return nil, err
		}

		if !isNumberLiteralChar(r, false) {
			_ = p.in.UnreadRune()
			break
		}

		ret += string(r)
	}

	return numberLiteralToken(ret), nil
}

func (p *sysIL4Parser) nextComment() (token, error) {
	ret := "#"

	for {
		r, _, err := p.in.ReadRune()
		if err != nil {
			return nil, err
		}

		if r == '\n' {
			_ = p.in.UnreadRune()
			break
		}

		ret += string(r)
	}

	return commentToken(ret), nil
}

func (p *sysIL4Parser) nextToken() (token, error) {
	if len(p.tokens) > 0 {
		ret := p.tokens[len(p.tokens)-1]
		p.tokens = p.tokens[:len(p.tokens)-1]
		return ret, nil
	}

	r, _, err := p.in.ReadRune()
	if err != nil {
		return nil, err
	}

	switch true {
	case isIdentifierChar(r, true):
		return p.nextIdentifier(r)
	case isWhitespaceChar(r):
		return p.nextWhitespace(r)
	case isNumberLiteralChar(r, true):
		return p.nextNumberLiteral(r)
	case r == '{':
		return specialOpenBlock, nil
	case r == '}':
		return specialCloseBlock, nil
	case r == '=':
		return specialEquals, nil
	case r == '[':
		return specialOpenSquareBracket, nil
	case r == ']':
		return specialCloseSquareBracket, nil
	case r == '-':
		return specialMinus, nil
	case r == '+':
		return specialPlus, nil
	case r == '$':
		ident, err := p.nextIdentifier(' ')
		if err != nil {
			return nil, err
		}

		return referenceToken(ident.(identifierToken)[1:]), nil
	case r == '#':
		return p.nextComment()
	default:
		return nil, fmt.Errorf("unexpected rune: %q", r)
	}
}

func (p *sysIL4Parser) unreadToken(tk token) {
	p.tokens = append(p.tokens, tk)
}

func (p *sysIL4Parser) nextSignificantToken() (token, error) {
	for {
		tk, err := p.nextToken()
		if err != nil {
			return nil, err
		}

		switch tk.(type) {
		case whitespaceToken:
			continue
		case commentToken:
			continue
		default:
			return tk, nil
		}
	}
}

func (p *sysIL4Parser) nextKeyword() (keywordToken, error) {
	tk, err := p.nextSignificantToken()
	if err != nil {
		return "", err
	}

	if _, ok := tk.(keywordToken); !ok {
		return "", fmt.Errorf("expected keyword, got %T", tk)
	}

	return tk.(keywordToken), nil
}

func (p *sysIL4Parser) expectToken(expected token) error {
	tk, err := p.nextSignificantToken()
	if err != nil {
		return err
	}

	if tk != expected {
		return fmt.Errorf("expected %T(%+v), got %T(%+v)",
			expected, expected, tk, tk)
	}

	return nil
}

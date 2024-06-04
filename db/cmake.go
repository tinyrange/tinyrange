package db

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"path"
	"regexp"
	"strings"

	"github.com/alecthomas/participle"
	"github.com/alecthomas/participle/lexer"
	cmakeLexer "github.com/kythe/llvmbzlgen/cmakelib/lexer"
	ast "github.com/tinyrange/pkg2/third_party/llvmbzlgen"
	"go.starlark.net/starlark"
)

type evaluator struct {
	parent *evaluator
	values *starlark.Dict
}

// Get implements ast.Bindings.
func (eval *evaluator) Get(k string) string {
	v, ok, err := eval.values.Get(starlark.String(k))
	if err != nil {
		slog.Error("CMakeEvaluator: failed to get", "k", k, "err", err)
	}

	if !ok {
		if eval.parent != nil {
			return eval.parent.Get(k)
		} else {
			return ""
		}
	}

	str, ok := starlark.AsString(v)
	if !ok {
		return v.String()
	} else {
		return str
	}
}

// GetCache implements ast.Bindings.
func (eval *evaluator) GetCache(k string) string {
	v, ok, err := eval.values.Get(starlark.String(k))
	if err != nil {
		slog.Error("CMakeEvaluator: failed to get", "k", k, "err", err)
	}

	if !ok {
		if eval.parent != nil {
			return eval.parent.GetCache(k)
		} else {
			return ""
		}
	}

	str, ok := starlark.AsString(v)
	if !ok {
		return v.String()
	} else {
		return str
	}
}

// GetEnv implements ast.Bindings.
func (eval *evaluator) GetEnv(k string) string {
	v, ok, err := eval.values.Get(starlark.String(k))
	if err != nil {
		slog.Error("CMakeEvaluator: failed to get", "k", k, "err", err)
	}

	if !ok {
		if eval.parent != nil {
			return eval.parent.GetEnv(k)
		} else {
			return ""
		}
	}

	str, ok := starlark.AsString(v)
	if !ok {
		return v.String()
	} else {
		return str
	}
}

var (
	_ ast.Bindings = &evaluator{}
)

type CMakeStatement interface {
	cmakeStatement()

	Eval(eval *CMakeEvaluatorScope) error
}

type CommandStatement struct {
	ast.CommandInvocation
}

// Eval implements CMakeStatement.
func (c *CommandStatement) Eval(eval *CMakeEvaluatorScope) error {
	if macro, ok := eval.eval.macros[c.Name]; ok {
		return macro(eval, c.Arguments.Values)
	}

	args := c.Arguments.Eval(eval.evaluator)

	return eval.eval.evalCommand(eval, c.Name, args)
}

// cmakeStatement implements CMakeStatement.
func (c *CommandStatement) cmakeStatement() { panic("unimplemented") }

func (c *CommandStatement) String() string {
	return fmt.Sprintf("Command{%s}", c.Name)
}

type ifStatementNodeType int

const (
	OP_UNARY ifStatementNodeType = iota
	OP_BINARY
	OP_VALUE
)

type ifStatementNode struct {
	Type        ifStatementNodeType
	StringValue string
	Value       ast.Argument
	Left        *ifStatementNode
	Right       *ifStatementNode
}

func (n *ifStatementNode) String() string {
	switch n.Type {
	case OP_UNARY:
		return fmt.Sprintf("(%s %s)", n.StringValue, n.Left)
	case OP_BINARY:
		return fmt.Sprintf("(%s %s %s)", n.Left, n.StringValue, n.Right)
	case OP_VALUE:
		return strings.Join(n.Value.Eval(nil), "")
	default:
		return "<unknown>"
	}
}

func (n *ifStatementNode) evalArg(eval *CMakeEvaluatorScope) (string, error) {
	arg := n.Value
	switch {
	case arg.QuotedArgument != nil:
		return strings.Join(arg.QuotedArgument.Eval(eval.evaluator), " "), nil
	case arg.UnquotedArgument != nil:
		val := strings.Join(arg.UnquotedArgument.Eval(eval.evaluator), " ")

		return eval.evaluator.Get(val), nil
	case arg.BracketArgument != nil:
		return strings.Join(arg.BracketArgument.Eval(eval.evaluator), " "), nil
	case arg.ArgumentList != nil:
		// Include the parens, but only for nested argument lists.
		values := []string{"("}
		values = append(values, arg.ArgumentList.Eval(eval.evaluator)...)
		return strings.Join(append(values, ")"), " "), nil
	}
	panic("Missing concrete argument!")
}

func (n *ifStatementNode) evalString(eval *CMakeEvaluatorScope) (string, error) {
	if n.Type != OP_VALUE {
		return "", fmt.Errorf("n.Type != OP_VALUE")
	}

	return n.evalArg(eval)
}

func (n *ifStatementNode) Eval(eval *CMakeEvaluatorScope) (bool, error) {
	switch n.Type {
	case OP_UNARY:
		switch n.StringValue {
		case "NOT":
			val, err := n.Left.Eval(eval)
			if err != nil {
				return false, err
			}

			return !val, nil
		case "COMMAND":
			val, err := n.Left.evalString(eval)
			if err != nil {
				return false, err
			}

			_, ok := eval.eval.commands[val]
			return ok, nil
		case "EXISTS":
			val, err := n.Left.evalString(eval)
			if err != nil {
				return false, err
			}

			_, ok, err := eval.eval.sourceRoot.openChild(val, false)
			if err != nil {
				return false, err
			}
			return ok, nil
		case "POLICY":
			val := n.Left.String()

			switch val {
			case "CMP0116":
				return false, nil
			default:
				return false, fmt.Errorf("unknown policy: %s", val)
			}
		default:
			return false, fmt.Errorf("unimplemented unary op: %s", n.StringValue)
		}
	case OP_BINARY:
		switch n.StringValue {
		case "MATCHES":
			lhs, err := n.Left.evalString(eval)
			if err != nil {
				return false, err
			}

			rhs, err := n.Right.evalString(eval)
			if err != nil {
				return false, err
			}

			slog.Info("MATCHES", "lhs", lhs, "rhs", rhs)

			return lhs == rhs, nil
		case "STREQUAL":
			lhs, err := n.Left.evalString(eval)
			if err != nil {
				return false, err
			}

			rhs, err := n.Right.evalString(eval)
			if err != nil {
				return false, err
			}

			slog.Info("STREQUAL", "lhs", lhs, "rhs", rhs)

			return lhs == rhs, nil
		case "VERSION_EQUAL":
			lhs, err := n.Left.evalString(eval)
			if err != nil {
				return false, err
			}

			rhs, err := n.Right.evalString(eval)
			if err != nil {
				return false, err
			}

			slog.Info("VERSION_EQUAL", "lhs", lhs, "rhs", rhs)

			return lhs == rhs, nil
		case "VERSION_GREATER_EQUAL":
			lhs, err := n.Left.evalString(eval)
			if err != nil {
				return false, err
			}

			rhs, err := n.Right.evalString(eval)
			if err != nil {
				return false, err
			}

			slog.Info("VERSION_GREATER_EQUAL", "lhs", lhs, "rhs", rhs)

			return lhs == rhs, nil
		case "AND":
			lhs, err := n.Left.Eval(eval)
			if err != nil {
				return false, err
			}

			rhs, err := n.Right.Eval(eval)
			if err != nil {
				return false, err
			}

			return lhs && rhs, nil
		case "OR":
			lhs, err := n.Left.Eval(eval)
			if err != nil {
				return false, err
			}

			rhs, err := n.Right.Eval(eval)
			if err != nil {
				return false, err
			}

			return lhs || rhs, nil
		default:
			return false, fmt.Errorf("unimplemented binary op: %s", n.StringValue)
		}
	case OP_VALUE:
		val, err := n.evalString(eval)
		if err != nil {
			return false, err
		}

		return val != "", nil
	}
	panic("unimplemented")
}

type ifStatementToken struct {
	Value ast.Argument
}

func (tk ifStatementToken) Type() string {
	return tk.Value.Eval(nil)[0]
}

func (tk ifStatementToken) IsBinary() bool {
	switch tk.Type() {
	case "EQUAL":
		return true
	case "LESS":
		return true
	case "LESS_EQUAL":
		return true
	case "GREATER":
		return true
	case "GREATER_EQUAL":
		return true
	case "STREQUAL":
		return true
	case "STRLESS":
		return true
	case "STRLESS_EQUAL":
		return true
	case "STRGREATER":
		return true
	case "STRGREATER_EQUAL":
		return true
	case "VERSION_EQUAL":
		return true
	case "VERSION_LESS":
		return true
	case "VERSION_LESS_EQUAL":
		return true
	case "VERSION_GREATER":
		return true
	case "VERSION_GREATER_EQUAL":
		return true
	case "PATH_EQUAL":
		return true
	case "MATCHES":
		return true
	case "AND":
		return true
	case "OR":
		return true
	default:
		return false
	}
}

func (tk ifStatementToken) IsUnary() bool {
	switch tk.Type() {
	case "EXISTS":
		return true
	case "COMMAND":
		return true
	case "DEFINED":
		return true
	case "POLICY":
		return true
	case "NOT":
		return true
	default:
		return false
	}
}

// precedenceParser structure
type precedenceParser struct {
	tokens     []ifStatementToken
	position   int
	stack      []*ifStatementNode
	operators  []ifStatementToken
	precedence map[string]int
}

// newIfStatementParser creates a new parser instance
func newIfStatementParser(args []ast.Argument) *precedenceParser {
	var tokens []ifStatementToken
	for _, arg := range args {
		tokens = append(tokens, ifStatementToken{Value: arg})
	}

	return &precedenceParser{
		tokens:   tokens,
		position: 0,
		stack:    []*ifStatementNode{},
		precedence: map[string]int{
			"EXISTS":                4,
			"COMMAND":               4,
			"DEFINED":               4,
			"POLICY":                4,
			"EQUAL":                 3,
			"LESS":                  3,
			"LESS_EQUAL":            3,
			"GREATER":               3,
			"GREATER_EQUAL":         3,
			"STREQUAL":              3,
			"STRLESS":               3,
			"STRLESS_EQUAL":         3,
			"STRGREATER":            3,
			"STRGREATER_EQUAL":      3,
			"VERSION_EQUAL":         3,
			"VERSION_LESS":          3,
			"VERSION_LESS_EQUAL":    3,
			"VERSION_GREATER":       3,
			"VERSION_GREATER_EQUAL": 3,
			"PATH_EQUAL":            3,
			"MATCHES":               3,
			"NOT":                   2,
			"AND":                   1,
			"OR":                    1,
		},
	}
}

// Parse parses the tokens into an AST
func (p *precedenceParser) Parse() *ifStatementNode {
	expectUnary := true

	for p.position < len(p.tokens) {
		token := p.tokens[p.position]
		p.position++

		switch {
		case token.IsUnary():
			if expectUnary {
				p.operators = append(p.operators, token)
			} else {
				for len(p.operators) > 0 && p.precedence[p.operators[len(p.operators)-1].Type()] >= p.precedence[token.Type()] {
					p.stack = append(p.stack, p.popOperator())
				}
				p.operators = append(p.operators, token)
			}
			expectUnary = true
		case token.IsBinary():
			for len(p.operators) > 0 && p.precedence[p.operators[len(p.operators)-1].Type()] >= p.precedence[token.Type()] {
				p.stack = append(p.stack, p.popOperator())
			}
			p.operators = append(p.operators, token)
			expectUnary = true
		default:
			p.stack = append(p.stack, &ifStatementNode{Type: OP_VALUE, Value: token.Value})
			expectUnary = false
		}
	}

	for len(p.operators) > 0 {
		p.stack = append(p.stack, p.popOperator())
	}

	// slog.Info("", "stack", p.stack)

	if len(p.stack) != 1 {
		panic("Invalid expression")
	}

	return p.stack[0]
}

// popOperator pops an operator and creates a binary or unary node
func (p *precedenceParser) popOperator() *ifStatementNode {
	op := p.operators[len(p.operators)-1]
	p.operators = p.operators[:len(p.operators)-1]

	if op.IsUnary() {
		// Handle unary operator
		right := p.stackPop()
		return &ifStatementNode{
			Type:        OP_UNARY,
			Value:       op.Value,
			StringValue: op.Type(),
			Left:        right,
		}
	}

	if len(p.stack) < 2 {
		panic("Invalid expression")
	}

	right := p.stackPop()
	left := p.stackPop()

	return &ifStatementNode{
		Type:        OP_BINARY,
		Value:       op.Value,
		StringValue: op.Type(),
		Left:        left,
		Right:       right,
	}
}

// stackPop pops a node from the stack
func (p *precedenceParser) stackPop() *ifStatementNode {
	if len(p.stack) == 0 {
		panic("Invalid expression")
	}
	var n *ifStatementNode
	n, p.stack = p.stack[len(p.stack)-1], p.stack[:len(p.stack)-1]
	return n
}

type IfStatement struct {
	ast.CommandInvocation

	Body []CMakeStatement
	Else []CMakeStatement
}

func (i *IfStatement) evalCondition(eval *CMakeEvaluatorScope) (bool, error) {
	args := i.Arguments.Values

	parser := newIfStatementParser(args)

	node := parser.Parse()

	return node.Eval(eval)
}

// Eval implements CMakeStatement.
func (i *IfStatement) Eval(eval *CMakeEvaluatorScope) error {
	cond, err := i.evalCondition(eval)
	if err != nil {
		return err
	}

	if cond {
		return eval.eval.evalBlock(eval, i.Body)
	} else {
		return eval.eval.evalBlock(eval, i.Else)
	}
}

// cmakeStatement implements CMakeStatement.
func (i *IfStatement) cmakeStatement() { panic("unimplemented") }

func (i *IfStatement) String() string {
	return fmt.Sprintf("If then %+v else %+v", i.Body, i.Else)
}

type MacroStatement struct {
	ast.CommandInvocation

	Contents string

	Filename string
	StartPos lexer.Position
	EndPos   lexer.Position
}

var MACRO_MATCHER = regexp.MustCompile(`\$\{([A-Z0-9]+)\}`)

func (m *MacroStatement) rawArgument(arg ast.Argument) string {
	switch {
	case arg.QuotedArgument != nil:
		ret := "\""
		for _, e := range arg.QuotedArgument.Elements {
			if e.Ref != nil {
				ret += "${"
				for _, ele := range e.Ref.Elements {
					if ele.Ref != nil {
						panic("ele.Ref != nil")
					}
					ret += ele.Text
				}
				ret += "}"
			} else {
				ret += e.Text
			}
		}
		return ret + "\""
	case arg.UnquotedArgument != nil:
		ret := ""
		for _, e := range arg.UnquotedArgument.Elements {
			if e.Ref != nil {
				panic("e.Ref != nil")
			}
			ret += e.Text
		}
		return ret
	case arg.BracketArgument != nil:
		panic("BracketArgument")
	case arg.ArgumentList != nil:
		panic("ArgumentList")
	}
	panic("Missing concrete argument!")
}

func (m *MacroStatement) rawArguments(args []ast.Argument) string {
	// Return the raw version of args.
	var ret []string

	for _, arg := range args {
		ret = append(ret, m.rawArgument(arg))
	}

	return strings.Join(ret, " ")
}

func (m *MacroStatement) evalMacro(scope *CMakeEvaluatorScope, args []ast.Argument) error {
	// perform macro replacement.
	frag := MACRO_MATCHER.ReplaceAllStringFunc(m.Contents, func(s string) string {
		name := s[2 : len(s)-1]

		if name == "ARGV" {
			return m.rawArguments(args)
		} else {
			return s
		}
	})

	slog.Info("macro eval", "name", m.Name, "frag", frag)

	// parse the new code fragment.
	return scope.eval.evalFragment(scope, frag)
}

// Eval implements CMakeStatement.
func (m *MacroStatement) Eval(eval *CMakeEvaluatorScope) error {
	args := m.Arguments.Eval(eval.evaluator)

	if len(args) == 1 {
		name := args[0]

		f, ok, err := eval.eval.open(m.Filename)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("file %s not found", m.Filename)
		}

		fh, err := f.Open()
		if err != nil {
			return err
		}
		defer fh.Close()

		contents, err := io.ReadAll(fh)
		if err != nil {
			return err
		}

		lines := strings.Split(string(contents), "\n")

		m.Contents = strings.Join(lines[m.StartPos.Line:m.EndPos.Line-1], "\n")

		eval.eval.macros[name] = func(scope *CMakeEvaluatorScope, args []ast.Argument) error {
			return m.evalMacro(scope, args)
		}

		return nil
	} else {
		return fmt.Errorf("macro not implemented: len(args) = %d", len(args))
	}
}

// cmakeStatement implements CMakeStatement.
func (m *MacroStatement) cmakeStatement() { panic("unimplemented") }

type FunctionStatement struct {
	ast.CommandInvocation

	body []ast.CommandInvocation
}

// Eval implements CMakeStatement.
func (f *FunctionStatement) Eval(eval *CMakeEvaluatorScope) error {
	args := f.Arguments.Eval(eval.evaluator)

	name := args[0]

	eval.eval.commands[name] = func(scope *CMakeEvaluatorScope, args []string) error {
		slog.Info("call to user defined function", "name", name, "args", args)
		return nil
	}

	return nil
}

// cmakeStatement implements CMakeStatement.
func (f *FunctionStatement) cmakeStatement() { panic("unimplemented") }

var (
	_ CMakeStatement = &CommandStatement{}
	_ CMakeStatement = &IfStatement{}
	_ CMakeStatement = &MacroStatement{}
	_ CMakeStatement = &FunctionStatement{}
)

type CMakeCommand func(scope *CMakeEvaluatorScope, args []string) error
type CMakeMacro func(scope *CMakeEvaluatorScope, args []ast.Argument) error

type CMakeEvaluatorScope struct {
	eval *CMakeEvaluator

	evaluator *evaluator
}

// Get implements starlark.HasSetKey.
func (eval *CMakeEvaluatorScope) Get(k starlark.Value) (v starlark.Value, found bool, err error) {
	return eval.evaluator.values.Get(k)
}

// SetKey implements starlark.HasSetKey.
func (eval *CMakeEvaluatorScope) SetKey(k starlark.Value, v starlark.Value) error {
	return eval.evaluator.values.SetKey(k, v)
}

func (eval *CMakeEvaluatorScope) childScope(dirname string) *CMakeEvaluatorScope {
	child := &CMakeEvaluatorScope{
		eval: eval.eval,
		evaluator: &evaluator{
			parent: eval.evaluator,
			values: starlark.NewDict(32),
		},
	}

	if dirname != "" {
		child.Set("CMAKE_CURRENT_LIST_DIR", dirname)
	}

	return child
}

func (eval *CMakeEvaluatorScope) Set(k string, v string) {
	eval.evaluator.values.SetKey(starlark.String(k), starlark.String(v))
}

func (*CMakeEvaluatorScope) String() string { return "CMakeEvaluatorScope" }
func (*CMakeEvaluatorScope) Type() string   { return "CMakeEvaluatorScope" }
func (*CMakeEvaluatorScope) Hash() (uint32, error) {
	return 0, fmt.Errorf("CMakeEvaluatorScope is not hashable")
}
func (*CMakeEvaluatorScope) Truth() starlark.Bool { return starlark.True }
func (*CMakeEvaluatorScope) Freeze()              {}

var (
	_ starlark.Value     = &CMakeEvaluatorScope{}
	_ starlark.HasSetKey = &CMakeEvaluatorScope{}
)

type CMakeEvaluator struct {
	base       string
	sourceRoot *StarDirectory

	commands map[string]CMakeCommand
	macros   map[string]CMakeMacro
}

func (eval *CMakeEvaluator) parseIfStatement(filename string, rest []ast.CommandInvocation) (CMakeStatement, []ast.CommandInvocation, error) {
	if rest[0].Name != "if" {
		return nil, nil, fmt.Errorf("parseIfStatement: rest[0].Name != \"if\"")
	}

	ret := &IfStatement{
		CommandInvocation: rest[0],
	}

	target := ret

	inElse := false

	rest = rest[1:]

outer:
	for {
		for i, cmd := range rest {
			if cmd.Name == "endif" {
				// slog.Info("if", "body", ret.Body, "else", ret.Else)
				return ret, rest[i+1:], nil
			} else if cmd.Name == "else" {
				inElse = true
			} else if cmd.Name == "elseif" {
				if inElse {
					return nil, nil, fmt.Errorf("else must be the last clause")
				}

				newTarget := &IfStatement{
					CommandInvocation: cmd,
				}

				target.Else = []CMakeStatement{newTarget}

				target = newTarget
			} else if cmd.Name == "if" {
				stmt, newRest, err := eval.parseIfStatement(filename, rest[i:])
				if err != nil {
					return nil, nil, err
				}

				if inElse {
					target.Else = append(target.Else, stmt)
				} else {
					target.Body = append(target.Body, stmt)
				}

				rest = newRest

				continue outer
			} else if cmd.Name == "macro" {
				stmt, newRest, err := eval.parseMacroStatement(filename, rest[i:])
				if err != nil {
					return nil, nil, err
				}

				if inElse {
					target.Else = append(target.Else, stmt)
				} else {
					target.Body = append(target.Body, stmt)
				}

				rest = newRest

				continue outer
			} else if cmd.Name == "function" {
				stmt, newRest, err := eval.parseFunctionStatement(filename, rest[i:])
				if err != nil {
					return nil, nil, err
				}

				if inElse {
					target.Else = append(target.Else, stmt)
				} else {
					target.Body = append(target.Body, stmt)
				}

				rest = newRest

				continue outer
			} else {
				if inElse {
					target.Else = append(target.Else, &CommandStatement{CommandInvocation: cmd})
				} else {
					target.Body = append(target.Body, &CommandStatement{CommandInvocation: cmd})
				}
			}
		}
		break outer
	}

	return nil, nil, fmt.Errorf("could not find an endif before the file ended")
}

func (eval *CMakeEvaluator) parseMacroStatement(filename string, rest []ast.CommandInvocation) (CMakeStatement, []ast.CommandInvocation, error) {
	if rest[0].Name != "macro" {
		return nil, nil, fmt.Errorf("parseMacroStatement: rest[0].Name != \"macro\"")
	}

	ret := &MacroStatement{
		CommandInvocation: rest[0],
		Filename:          filename,
	}

	ret.StartPos = rest[0].Pos

	rest = rest[1:]

	for i, cmd := range rest {
		if cmd.Name == "endmacro" {
			ret.EndPos = cmd.Pos
			return ret, rest[i+1:], nil
		} else {
			continue
		}
	}

	return nil, nil, fmt.Errorf("could not find an endmacro before the file ended")
}

func (eval *CMakeEvaluator) parseFunctionStatement(filename string, rest []ast.CommandInvocation) (CMakeStatement, []ast.CommandInvocation, error) {
	if rest[0].Name != "function" {
		return nil, nil, fmt.Errorf("parseMacroStatement: rest[0].Name != \"macro\"")
	}

	ret := &FunctionStatement{
		CommandInvocation: rest[0],
	}

	rest = rest[1:]

	for i, cmd := range rest {
		if cmd.Name == "endfunction" {
			return ret, rest[i+1:], nil
		} else {
			ret.body = append(ret.body, cmd)
		}
	}

	return nil, nil, fmt.Errorf("could not find an endfunction before the file ended")
}

func (eval *CMakeEvaluator) parseStatement(filename string, cmds []ast.CommandInvocation) ([]CMakeStatement, error) {
	var ret []CMakeStatement

	for i, cmd := range cmds {
		if cmd.Name == "if" {
			stmt, newRest, err := eval.parseIfStatement(filename, cmds[i:])
			if err != nil {
				return nil, err
			}
			ret = append(ret, stmt)

			rest, err := eval.parseStatement(filename, newRest)
			if err != nil {
				return nil, err
			}

			return append(ret, rest...), nil
		} else if cmd.Name == "macro" {
			stmt, newRest, err := eval.parseMacroStatement(filename, cmds[i:])
			if err != nil {
				return nil, err
			}
			ret = append(ret, stmt)

			rest, err := eval.parseStatement(filename, newRest)
			if err != nil {
				return nil, err
			}

			return append(ret, rest...), nil
		} else if cmd.Name == "function" {
			stmt, newRest, err := eval.parseFunctionStatement(filename, cmds[i:])
			if err != nil {
				return nil, err
			}
			ret = append(ret, stmt)

			rest, err := eval.parseStatement(filename, newRest)
			if err != nil {
				return nil, err
			}

			return append(ret, rest...), nil
		} else {
			ret = append(ret, &CommandStatement{CommandInvocation: cmd})
		}
	}

	return ret, nil
}

func (eval *CMakeEvaluator) evalBlock(scope *CMakeEvaluatorScope, stmts []CMakeStatement) error {
	// slog.Info("evalBlock", "stmts", stmts)

	for _, stmt := range stmts {
		if err := stmt.Eval(scope); err != nil {
			return err
		}
	}

	return nil
}

func (eval *CMakeEvaluator) evalFile(scope *CMakeEvaluatorScope, filename string, ast *ast.CMakeFile) error {
	child := scope.childScope(path.Dir(filename))

	stmts, err := eval.parseStatement(filename, ast.Commands)
	if err != nil {
		return err
	}

	if err := eval.evalBlock(child, stmts); err != nil {
		return err
	}

	return nil
}

func (eval *CMakeEvaluator) evalCommand(scope *CMakeEvaluatorScope, name string, args []string) error {
	cmd, ok := eval.commands[name]
	if !ok {
		return fmt.Errorf("command %s not found", name)
	}

	return cmd(scope, args)
}

func (eval *CMakeEvaluator) open(path string) (StarFileIf, bool, error) {
	path = strings.TrimPrefix(path, eval.base)

	slog.Info("open", "path", path)

	return eval.sourceRoot.openChild(path, false)
}

func (eval *CMakeEvaluator) searchForFile(name string, paths []string) (StarFileIf, error) {
	slog.Info("searchForFile", "name", name, "paths", paths)
	child, ok, err := eval.open(name)
	if err != nil {
		return nil, err
	}
	if ok {
		return child, nil
	}

	for _, path := range paths {
		dir, ok, err := eval.open(path)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}

		if dir, ok := dir.(*StarDirectory); ok {
			for childName, child := range dir.entries {
				if childName == name {
					return child, nil
				}
			}
		} else {
			return nil, fmt.Errorf("item in search path is not a directory: %s", path)
		}
	}

	return nil, fmt.Errorf("file not found in paths: %s", name)
}

func (eval *CMakeEvaluator) evalInclude(scope *CMakeEvaluatorScope, name string, opts []string) error {
	slog.Info("include", "name", name, "opts", opts)

	searchPath := strings.Split(scope.evaluator.Get("CMAKE_MODULE_PATH"), " ")

	if !strings.HasSuffix(name, ".cmake") {
		name = name + ".cmake"
	}

	file, err := eval.searchForFile(name, searchPath)
	if err != nil {
		return err
	}

	if err := eval.evalFileIf(scope, file); err != nil {
		return err
	}

	return nil
}

func (eval *CMakeEvaluator) evalFileIf(scope *CMakeEvaluatorScope, f StarFileIf) error {
	fh, err := f.Open()
	if err != nil {
		return err
	}
	defer fh.Close()

	lexerDef := cmakeLexer.New()

	parser := participle.MustBuild(&ast.CMakeFile{}, participle.Lexer(lexerDef))

	ast := &ast.CMakeFile{}

	lex, err := lexerDef.Lex(fh)
	if err != nil {
		return err
	}

	peeker, err := lexer.Upgrade(lex)
	if err != nil {
		return err
	}

	// Do a quick scan of the lexed tokens to see if the file has anything in it.
	// If we reach the end before finding anything besides newlines then don't bother parsing and evaluating the file.
	i := 0
	for {
		tk, err := peeker.Peek(i)
		if err != nil {
			return err
		}
		if tk.String() == "\n" {
			i += 1
			continue
		}
		if tk.EOF() {
			return nil
		}
		break
	}

	if err := parser.ParseFromLexer(peeker, ast); err != nil {
		return fmt.Errorf("error parsing file %s: %w", f.Name(), err)
	}

	if err := eval.evalFile(scope, f.Name(), ast); err != nil {
		return fmt.Errorf("error evaluating file %s: %w", f.Name(), err)
	}

	return nil
}

func (eval *CMakeEvaluator) evalFragment(scope *CMakeEvaluatorScope, frag string) error {
	parser := ast.NewParser()

	ast, err := parser.Parse(bytes.NewReader([]byte(frag)))
	if err != nil {
		return err
	}

	stmts, err := eval.parseStatement("", ast.Commands)
	if err != nil {
		return err
	}

	if err := eval.evalBlock(scope, stmts); err != nil {
		return err
	}

	return nil
}

func evalCMake(f *StarDirectory, ctx *starlark.Dict, cmds *starlark.Dict) (starlark.Value, error) {
	top, ok, err := f.openChild("CMakeLists.txt", false)
	if err != nil {
		return starlark.None, err
	}
	if !ok {
		return starlark.None, fmt.Errorf("could not find CMakeLists.txt at the top of the archive")
	}

	fh, err := top.Open()
	if err != nil {
		return starlark.None, err
	}
	defer fh.Close()

	parser := ast.NewParser()

	ast, err := parser.Parse(fh)
	if err != nil {
		return starlark.None, err
	}

	eval := &CMakeEvaluator{
		base:       f.Name() + "/",
		sourceRoot: f,
		commands:   make(map[string]CMakeCommand),
		macros:     make(map[string]CMakeMacro),
	}

	eval.commands["include"] = func(scope *CMakeEvaluatorScope, args []string) error {
		return eval.evalInclude(scope, args[0], args[1:])
	}

	cmds.Entries(func(k, v starlark.Value) bool {
		kStr, ok := starlark.AsString(k)
		if !ok {
			slog.Warn("could not convert to string", "type", k.Type())
		}

		eval.commands[kStr] = func(scope *CMakeEvaluatorScope, args []string) error {
			var argValues starlark.Tuple

			for _, arg := range args {
				argValues = append(argValues, starlark.String(arg))
			}

			thread := &starlark.Thread{}

			_, err := starlark.Call(thread, v, starlark.Tuple{scope, argValues}, []starlark.Tuple{})
			if err != nil {
				return err
			}

			return nil
		}

		return true
	})

	scope := &CMakeEvaluatorScope{
		eval: eval,
		evaluator: &evaluator{
			values: ctx,
		},
	}

	if err := eval.evalFile(scope, top.Name(), ast); err != nil {
		return starlark.None, err
	}

	return starlark.None, fmt.Errorf("not implemented")
}

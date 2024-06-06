package cmake

import (
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"strconv"
	"strings"

	"github.com/alecthomas/participle/lexer"
	ast "github.com/tinyrange/pkg2/third_party/llvmbzlgen"
)

type CMakeStatement interface {
	cmakeStatement()

	Eval(eval *CMakeEvaluatorScope) error
}

type CMakeBlock struct {
	Stmt CMakeStatement
	Body []CMakeStatement
}

func (blk *CMakeBlock) String() string {
	return fmt.Sprintf("%T", blk.Stmt)
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

		_, err := strconv.Atoi(val)
		if err == nil {
			// If the value parses as a int then return the raw value.
			return val, nil
		} else {
			return eval.evaluator.Get(val), nil
		}
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

			_, ok, err := eval.eval.sourceRoot.OpenChild(val, false)
			if err != nil {
				// Ignore error
				return false, nil
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
		case "DEFINED":
			val := n.Left.Value.Eval(eval.evaluator)[0]

			return eval.evaluator.Defined(val), nil
		case "TARGET":
			// TODO(joshua): implement
			return false, nil
		case "IS_ABSOLUTE":
			// TODO(joshua): implement
			return false, nil
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
		case "IN_LIST":
			lhs, err := n.Left.evalString(eval)
			if err != nil {
				return false, err
			}

			rhs, err := n.Right.evalString(eval)
			if err != nil {
				return false, err
			}

			_ = lhs
			_ = rhs

			slog.Info("IN_LIST TODO", "left", lhs, "right", rhs)

			return false, nil
		case "EQUAL":
			lhs, err := n.Left.evalString(eval)
			if err != nil {
				return false, err
			}

			rhs, err := n.Right.evalString(eval)
			if err != nil {
				return false, err
			}

			slog.Info("EQUAL", "a", lhs, "b", rhs)

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

			slog.Info("AND", "a", lhs, "b", rhs)

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

		slog.Info("", "value", val)

		if val == "OFF" {
			return false, nil
		} else if val == "" {
			return false, nil
		} else {
			return true, nil
		}
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
	case "IN_LIST":
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
	case "TARGET":
		return true
	case "IS_ABSOLUTE":
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
			"TARGET":                4,
			"IS_ABSOLUTE":           4,
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
			"IN_LIST":               3,
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

	Body *CMakeBlock
	Else *CMakeBlock
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

func (m *MacroStatement) evalMacro(scope *CMakeEvaluatorScope, macroArgs []string, args []ast.Argument) error {
	// perform macro replacement.
	frag := MACRO_MATCHER.ReplaceAllStringFunc(m.Contents, func(s string) string {
		name := s[2 : len(s)-1]

		if name == "ARGV" {
			return m.rawArguments(args)
		}

		for i, arg := range macroArgs {
			if arg == name {
				return m.rawArguments([]ast.Argument{args[i]})
			}
		}

		return s
	})

	// slog.Info("macro eval", "name", m.Name, "frag", frag)

	// parse the new code fragment.
	return scope.eval.evalFragment(scope, frag)
}

// Eval implements CMakeStatement.
func (m *MacroStatement) Eval(eval *CMakeEvaluatorScope) error {
	args := m.Arguments.Eval(eval.evaluator)

	name := args[0]
	macroArgs := args[1:]

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
		return m.evalMacro(scope, macroArgs, args)
	}

	return nil
}

// cmakeStatement implements CMakeStatement.
func (m *MacroStatement) cmakeStatement() { panic("unimplemented") }

type FunctionStatement struct {
	ast.CommandInvocation

	Body *CMakeBlock
}

// Eval implements CMakeStatement.
func (f *FunctionStatement) Eval(eval *CMakeEvaluatorScope) error {
	args := f.Arguments.Eval(eval.evaluator)

	name := args[0]
	argNames := args[1:]

	eval.eval.commands[name] = func(scope *CMakeEvaluatorScope, callArgs []string) error {
		child := scope.childScope(true, scope.evaluator.dirname)

		for i, name := range argNames {
			child.Set(name, callArgs[i])
		}

		slog.Info("call to user defined function", "name", name, "argNames", argNames, "callArgs", callArgs)

		return scope.eval.evalBlock(child, f.Body)
	}

	return nil
}

// cmakeStatement implements CMakeStatement.
func (f *FunctionStatement) cmakeStatement() { panic("unimplemented") }

type ForEachStatement struct {
	ast.CommandInvocation

	Body *CMakeBlock
}

// Eval implements CMakeStatement.
func (f *ForEachStatement) Eval(eval *CMakeEvaluatorScope) error {
	args := f.Arguments.Eval(eval.evaluator)

	// slog.Info("foreach unimplemented", "args", args)

	if len(args) == 0 {
		return fmt.Errorf("foreach with no arguments")
	}

	name := args[0]
	var vals []string

	if len(args) > 2 && args[1] == "IN" {
		if len(args) > 3 && args[2] == "LISTS" {
			lst := args[3]

			vals = strings.Split(eval.evaluator.Get(lst), ";")
		} else {
			return fmt.Errorf("unimplemented foreach variation 2: %+v", args)
		}
	} else {
		vals = args[1:]
	}

	// Don't loop though empty lists.
	if len(vals) == 1 && vals[0] == "" {
		return nil
	}

	for _, val := range vals {
		child := eval.childScope(false, eval.evaluator.dirname)

		child.Set(name, val)

		err := child.eval.evalBlock(child, f.Body)
		if err == ErrControlBreak {
			break
		} else if err == ErrControlContinue {
			continue
		} else if err != nil {
			return err
		}
	}

	return nil
}

// cmakeStatement implements CMakeStatement.
func (f *ForEachStatement) cmakeStatement() { panic("unimplemented") }

var (
	_ CMakeStatement = &CommandStatement{}
	_ CMakeStatement = &IfStatement{}
	_ CMakeStatement = &MacroStatement{}
	_ CMakeStatement = &FunctionStatement{}
	_ CMakeStatement = &ForEachStatement{}
)

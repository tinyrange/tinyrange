package cmake

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"strings"

	"github.com/alecthomas/participle"
	"github.com/alecthomas/participle/lexer"
	cmakeLexer "github.com/kythe/llvmbzlgen/cmakelib/lexer"
	"github.com/tinyrange/pkg2/pkg/common"
	ast "github.com/tinyrange/pkg2/third_party/llvmbzlgen"
	"go.starlark.net/starlark"
)

var (
	ErrControlBreak    = errors.New("break")
	ErrControlContinue = errors.New("continue")
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

func (eval *CMakeEvaluatorScope) childScope(newScope bool, dirname string) *CMakeEvaluatorScope {
	child := &CMakeEvaluatorScope{
		eval:      eval.eval,
		evaluator: eval.evaluator.childScope(newScope, dirname),
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
	sourceRoot common.StarDirectory

	commands map[string]CMakeCommand
	macros   map[string]CMakeMacro
}

type blockStack []*CMakeBlock

func (s blockStack) Push(v *CMakeBlock) blockStack {
	return append(s, v)
}

func (s blockStack) Pop() (blockStack, *CMakeBlock) {
	// FIXME: What do we do if the stack is empty, though?

	l := len(s)
	return s[:l-1], s[l-1]
}

func (s blockStack) Peek() *CMakeBlock {
	l := len(s)
	return s[l-1]
}

func (eval *CMakeEvaluator) parseStatement(filename string, cmds []ast.CommandInvocation) (*CMakeBlock, error) {
	ret := &CMakeBlock{}

	stack := make(blockStack, 0)

	stack = stack.Push(ret)

	skip := 0

	for i, cmd := range cmds {
		if skip > 0 {
			skip -= 1
			continue
		}

		switch cmd.Name {
		case "if":
			stmt := &IfStatement{CommandInvocation: cmd}
			stmt.Body = &CMakeBlock{Stmt: stmt}
			stmt.Else = &CMakeBlock{Stmt: stmt}

			top := stack.Peek()
			top.Body = append(top.Body, stmt)

			stack = stack.Push(stmt.Body)
		case "elseif":
			var block *CMakeBlock

			stack, block = stack.Pop()

			ifStmt, ok := block.Stmt.(*IfStatement)
			if !ok {
				// TOOD(joshua): Make this message more useful.
				return nil, fmt.Errorf("invalid structure")
			}

			if block == ifStmt.Else {
				return nil, fmt.Errorf("elseif has to come before else")
			}

			stmt := &IfStatement{CommandInvocation: cmd}
			stmt.Body = &CMakeBlock{Stmt: stmt}
			stmt.Else = &CMakeBlock{Stmt: stmt}

			ifStmt.Else.Body = append(ifStmt.Else.Body, stmt)

			stack = stack.Push(stmt.Body)
		case "else":
			var block *CMakeBlock

			stack, block = stack.Pop()

			ifStmt, ok := block.Stmt.(*IfStatement)
			if !ok {
				// TOOD(joshua): Make this message more useful.
				return nil, fmt.Errorf("invalid structure")
			}

			if block == ifStmt.Else {
				return nil, fmt.Errorf("if statements can not have more than 1 else block")
			}

			stack = stack.Push(ifStmt.Else)
		case "endif":
			var block *CMakeBlock

			stack, block = stack.Pop()

			_, ok := block.Stmt.(*IfStatement)
			if !ok {
				// TOOD(joshua): Make this message more useful.
				return nil, fmt.Errorf("expected IfStatement for endif got %T", block.Stmt)
			}
		case "macro":
			stmt := &MacroStatement{
				CommandInvocation: cmd,
				Filename:          filename,
			}

			stmt.StartPos = cmd.Pos

			for i2, cmd := range cmds[i+1:] {
				if cmd.Name == "endmacro" {
					stmt.EndPos = cmd.Pos

					// skip the body of the macro.
					skip = i2 + 1
					break
				} else if cmd.Name == "macro" {
					return nil, fmt.Errorf("nested macros are not supported")
				} else {
					continue
				}
			}

			top := stack.Peek()
			top.Body = append(top.Body, stmt)
		case "function":
			stmt := &FunctionStatement{CommandInvocation: cmd}
			stmt.Body = &CMakeBlock{Stmt: stmt}

			top := stack.Peek()
			top.Body = append(top.Body, stmt)

			stack = stack.Push(stmt.Body)
		case "endfunction":
			var block *CMakeBlock

			stack, block = stack.Pop()

			_, ok := block.Stmt.(*FunctionStatement)
			if !ok {
				// TOOD(joshua): Make this message more useful.
				return nil, fmt.Errorf("expected FunctionStatement for endfunction got %T", block.Stmt)
			}
		case "foreach":
			stmt := &ForEachStatement{CommandInvocation: cmd}
			stmt.Body = &CMakeBlock{Stmt: stmt}

			top := stack.Peek()
			top.Body = append(top.Body, stmt)

			stack = stack.Push(stmt.Body)
		case "endforeach":
			var block *CMakeBlock

			stack, block = stack.Pop()

			_, ok := block.Stmt.(*ForEachStatement)
			if !ok {
				// TOOD(joshua): Make this message more useful.
				return nil, fmt.Errorf("expected ForEachStatement for endforeach got %T", block.Stmt)
			}
		default:
			top := stack.Peek()
			top.Body = append(top.Body, &CommandStatement{CommandInvocation: cmd})
		}
	}

	if len(stack) != 1 {
		return nil, fmt.Errorf("length of block stack is not 1")
	}

	return ret, nil
}

func (eval *CMakeEvaluator) evalBlock(scope *CMakeEvaluatorScope, block *CMakeBlock) error {
	// slog.Info("evalBlock", "stmts", stmts)

	for _, stmt := range block.Body {
		if err := stmt.Eval(scope); err != nil {
			return err
		}
	}

	return nil
}

func (eval *CMakeEvaluator) evalFile(scope *CMakeEvaluatorScope, newScope bool, filename string, ast *ast.CMakeFile) error {
	child := scope.childScope(newScope, path.Dir(filename))

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

func (eval *CMakeEvaluator) open(path string) (common.StarFileIf, bool, error) {
	path = strings.TrimPrefix(path, eval.base)

	slog.Info("open", "path", path)

	return eval.sourceRoot.OpenChild(path, false)
}

func (eval *CMakeEvaluator) searchForFile(name string, paths []string) (common.StarFileIf, error) {
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

		if dir, ok := dir.(common.StarDirectory); ok {
			for childName, child := range dir.Entries() {
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

	// Get the search path from the scope.
	searchPath := strings.Split(scope.evaluator.Get("CMAKE_MODULE_PATH"), ";")

	// Add the current directory to the search path.
	searchPath = append([]string{scope.evaluator.dirname}, searchPath...)

	if !strings.HasSuffix(name, ".cmake") {
		name = name + ".cmake"
	}

	file, err := eval.searchForFile(name, searchPath)
	if err != nil {
		return err
	}

	if err := eval.evalFileIf(scope, false, file); err != nil {
		return err
	}

	return nil
}

func (eval *CMakeEvaluator) evalFileIf(scope *CMakeEvaluatorScope, newScope bool, f common.StarFileIf) error {
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

	if err := eval.evalFile(scope, newScope, f.Name(), ast); err != nil {
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

func (eval *CMakeEvaluator) addSubdirectory(scope *CMakeEvaluatorScope, dir string) error {
	dir = path.Join(scope.evaluator.dirname, dir)

	slog.Info("add_subdirectory", "dir", dir)

	f, found, err := eval.open(path.Join(dir, "CMakeLists.txt"))
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("could not find CMakeLists.txt at the top of the archive")
	}

	return eval.evalFileIf(scope, true, f)
}

func EvalCMake(f common.StarDirectory, ctx *starlark.Dict, cmds *starlark.Dict) (starlark.Value, error) {
	top, ok, err := f.OpenChild("CMakeLists.txt", false)
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

	eval.commands["add_subdirectory"] = func(scope *CMakeEvaluatorScope, args []string) error {
		return eval.addSubdirectory(scope, args[0])
	}

	eval.commands["break"] = func(scope *CMakeEvaluatorScope, args []string) error {
		return ErrControlBreak
	}

	eval.commands["continue"] = func(scope *CMakeEvaluatorScope, args []string) error {
		return ErrControlContinue
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

	if err := eval.evalFile(scope, false, top.Name(), ast); err != nil {
		return starlark.None, err
	}

	return starlark.None, fmt.Errorf("not implemented")
}

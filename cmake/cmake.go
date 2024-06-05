package cmake

import (
	"bytes"
	"fmt"
	"log/slog"
	"path"
	"strings"

	"github.com/alecthomas/participle"
	"github.com/alecthomas/participle/lexer"
	cmakeLexer "github.com/kythe/llvmbzlgen/cmakelib/lexer"
	"github.com/tinyrange/pkg2/db/common"
	ast "github.com/tinyrange/pkg2/third_party/llvmbzlgen"
	"go.starlark.net/starlark"
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
			} else if cmd.Name == "foreach" {
				stmt, newRest, err := eval.parseForEachStatement(filename, rest[i:])
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
		return nil, nil, fmt.Errorf("parseFunctionStatement: rest[0].Name != \"function\"")
	}

	ret := &FunctionStatement{
		CommandInvocation: rest[0],
	}

	rest = rest[1:]

	nesting := 0

	for i, cmd := range rest {
		if cmd.Name == "endfunction" && nesting == 0 {
			return ret, rest[i+1:], nil
		} else if cmd.Name == "endfunction" {
			ret.body = append(ret.body, cmd)
			nesting -= 1
		} else if cmd.Name == "function" {
			ret.body = append(ret.body, cmd)
			nesting += 1
		} else {
			ret.body = append(ret.body, cmd)
		}
	}

	return nil, nil, fmt.Errorf("could not find an endfunction before the file ended")
}

func (eval *CMakeEvaluator) parseForEachStatement(filename string, rest []ast.CommandInvocation) (CMakeStatement, []ast.CommandInvocation, error) {
	if rest[0].Name != "foreach" {
		return nil, nil, fmt.Errorf("parseForEachStatement: rest[0].Name != \"foreach\"")
	}

	ret := &ForEachStatement{
		CommandInvocation: rest[0],
	}

	rest = rest[1:]

	for i, cmd := range rest {
		if cmd.Name == "endforeach" {
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
		} else if cmd.Name == "foreach" {
			stmt, newRest, err := eval.parseForEachStatement(filename, cmds[i:])
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

	searchPath := strings.Split(scope.evaluator.Get("CMAKE_MODULE_PATH"), " ")

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

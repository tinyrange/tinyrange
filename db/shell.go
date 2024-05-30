package db

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/tinyrange/pkg2/third_party/kati"
	"go.starlark.net/starlark"
	"mvdan.cc/sh/v3/syntax"
)

type starDepNode struct {
	rule *kati.DepNode
}

// Attr implements starlark.HasAttrs.
func (t *starDepNode) Attr(name string) (starlark.Value, error) {
	if name == "depends" {
		var depends []starlark.Value

		for _, depend := range t.rule.Deps {
			depends = append(depends, &starDepNode{rule: depend})
		}

		return starlark.NewList(depends), nil
	} else if name == "raw_command" {
		return starlark.String(strings.Join(t.rule.Cmds, "\n")), nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (t *starDepNode) AttrNames() []string {
	return []string{"depends", "raw_command"}
}

func (t *starDepNode) String() string {
	return fmt.Sprintf("DepNode{%s, %s}", t.rule.Filename, t.rule.Output)
}
func (*starDepNode) Type() string { return "DepNode" }
func (*starDepNode) Hash() (uint32, error) {
	return 0, fmt.Errorf("DepNode is not hashable")
}
func (*starDepNode) Truth() starlark.Bool { return starlark.True }
func (*starDepNode) Freeze()              {}

var (
	_ starlark.Value    = &starDepNode{}
	_ starlark.HasAttrs = &starDepNode{}
)

type starDepGraph struct {
	graph *kati.DepGraph
}

// Attr implements starlark.HasAttrs.
func (t *starDepGraph) Attr(name string) (starlark.Value, error) {
	if name == "rules" {
		var rules []starlark.Value

		for _, rule := range t.graph.Nodes() {
			rules = append(rules, &starDepNode{rule: rule})
		}

		return starlark.NewList(rules), nil
	} else if name == "exec" {
		return starlark.NewBuiltin("DepGraph.exec", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				ctx          *ShellContext
				returnErrors bool
			)

			if err := starlark.UnpackArgs("DepGraph.exec", args, kwargs,
				"ctx", &ctx,
				"return_errors?", &returnErrors,
			); err != nil {
				return starlark.None, err
			}

			exec, err := kati.NewExecutor(ctx, &kati.ExecutorOpt{
				NumJobs: 1,
			})
			if err != nil {
				return starlark.None, err
			}

			if err := exec.Exec(t.graph, []string{}); err != nil {
				if returnErrors {
					return starlark.String(err.Error()), nil
				} else {
					return starlark.None, err
				}
			}

			return starlark.None, nil
		}), nil
	} else if name == "eval" {
		return starlark.NewBuiltin("DepGraph.eval", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				ctx          *ShellContext
				node         *starDepNode
				returnErrors bool
			)

			if err := starlark.UnpackArgs("DepGraph.eval", args, kwargs,
				"ctx", &ctx,
				"node", &node,
				"return_errors?", &returnErrors,
			); err != nil {
				return starlark.None, err
			}

			commands, err := kati.EvalCommands(ctx, node.rule, t.graph.Vars())
			if err != nil {
				return starlark.None, err
			}

			var ret []starlark.Value

			for _, cmd := range commands {
				ret = append(ret, starlark.String(cmd))
			}

			return starlark.NewList(ret), nil
		}), nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (t *starDepGraph) AttrNames() []string {
	return []string{"rules", "execute"}
}

func (t *starDepGraph) String() string { return "DepGraph" }
func (*starDepGraph) Type() string     { return "DepGraph" }
func (*starDepGraph) Hash() (uint32, error) {
	return 0, fmt.Errorf("DepGraph is not hashable")
}
func (*starDepGraph) Truth() starlark.Bool { return starlark.True }
func (*starDepGraph) Freeze()              {}

var (
	_ starlark.Value    = &starDepGraph{}
	_ starlark.HasAttrs = &starDepGraph{}
)

type fileStat struct {
	name string
	val  starlark.Value
}

// IsDir implements fs.FileInfo.
func (f *fileStat) IsDir() bool {
	return false
}

// ModTime implements fs.FileInfo.
func (f *fileStat) ModTime() time.Time {
	return time.Now()
}

// Mode implements fs.FileInfo.
func (f *fileStat) Mode() fs.FileMode {
	return fs.FileMode(0644)
}

// Name implements fs.FileInfo.
func (f *fileStat) Name() string {
	return f.name
}

// Size implements fs.FileInfo.
func (f *fileStat) Size() int64 {
	if str, ok := f.val.(starlark.String); ok {
		return int64(len(str))
	}
	return 0
}

// Sys implements fs.FileInfo.
func (f *fileStat) Sys() any {
	return nil
}

var (
	_ fs.FileInfo = &fileStat{}
)

type katiFile struct {
	io.Reader

	val  starlark.Value
	name string
}

// Chmod implements kati.File.
func (k *katiFile) Chmod(mode fs.FileMode) error {
	panic("unimplemented")
}

// Close implements kati.File.
func (k *katiFile) Close() error {
	return nil
}

// Readdirnames implements kati.File.
func (k *katiFile) Readdirnames(limit int) ([]string, error) {
	slog.Info("File.Readdirnames", "name", k.name)
	return []string{}, nil
}

// Write implements kati.File.
func (k *katiFile) Write(p []byte) (n int, err error) {
	panic("unimplemented")
}

var (
	_ kati.File = &katiFile{}
)

type shellCommand struct {
	name string
	f    *starlark.Function
}

func (cmd *shellCommand) Run(ctx *ShellContext, argv []string, stdin string) (CommandResult, error) {
	thread := &starlark.Thread{}

	var args starlark.Tuple

	for _, arg := range argv {
		// if arg != "" {
		args = append(args, starlark.String(arg))
		// }
	}

	// TODO(joshua): This is a massive hack. Contexts should be local to each subshell/command execution.
	ctx.stdin = stdin

	ret, err := starlark.Call(thread, cmd.f, starlark.Tuple{ctx, args}, []starlark.Tuple{})
	if err != nil {
		return emptyResult(), err
	}

	exitCode := 0

	if tup, ok := ret.(starlark.Tuple); ok {
		exitCode, err = starlark.AsInt32(tup[1])
		if err != nil {
			return emptyResult(), err
		}

		ret = tup[0]
	}

	str, ok := starlark.AsString(ret)
	if !ok {
		return emptyResult(), fmt.Errorf("value %T could not be converted to a string", ret)
	}

	result := newResult(str)

	return result.SetExitCode(exitCode), nil
}

type CommandResult struct {
	ExitCode int
	Stdout   string
}

func (res *CommandResult) SetExitCode(code int) CommandResult {
	res.ExitCode = code
	return *res
}

func newResult(stdout string) CommandResult {
	return CommandResult{Stdout: stdout}
}

func emptyResult() CommandResult {
	return newResult("")
}

type ShellContext struct {
	files           *starlark.Dict
	environ         *starlark.Dict
	state           *starlark.Dict
	stdin           string
	commands        map[string]*shellCommand
	fileNotFound    *starlark.Function
	commandNotFound *starlark.Function
}

func (p *ShellContext) getFile(filename string) (starlark.Value, error) {
	val, ok, err := p.files.Get(starlark.String(filename))
	if err != nil {
		return nil, err
	}
	if !ok {
		if p.fileNotFound != nil {
			thread := &starlark.Thread{}

			ret, err := starlark.Call(thread, p.fileNotFound, starlark.Tuple{p, starlark.String(filename)}, []starlark.Tuple{})
			if err != nil {
				return nil, err
			}

			if ret == starlark.None {
				return nil, fs.ErrNotExist
			}

			return nil, fmt.Errorf("file_not_found result not implemented")
		}

		return nil, fs.ErrNotExist
	}

	return val, nil
}

// Lstat implements kati.EnvironmentInterface.
func (p *ShellContext) Lstat(filename string) (fs.FileInfo, error) {
	panic("unimplemented")
}

// Open implements kati.EnvironmentInterface.
func (p *ShellContext) Open(filename string) (kati.File, error) {
	val, err := p.getFile(filename)
	if err != nil {
		return nil, err
	}

	contents, ok := starlark.AsString(val)
	if !ok {
		return nil, fmt.Errorf("can not get file contents as string")
	}

	return &katiFile{Reader: bytes.NewReader([]byte(contents)), val: val, name: filename}, nil
}

// Remove implements kati.EnvironmentInterface.
func (p *ShellContext) Remove(filename string) error {
	panic("unimplemented")
}

// Abspath implements kati.EnvironmentInterface.
func (*ShellContext) Abspath(p string) (string, error) {
	slog.Info("ShellContext.Abspath", "p", p)
	return filepath.Clean(p), nil
}

// EvalSymlinks implements kati.EnvironmentInterface.
func (*ShellContext) EvalSymlinks(p string) (string, error) {
	slog.Info("ShellContext.EvalSymlinks", "p", p)
	return p, nil
}

func (p *ShellContext) Environ() []string {
	var ret []string

	p.environ.Entries(func(k, v starlark.Value) bool {
		kStr, ok := starlark.AsString(k)
		if !ok {
			return false
		}

		vStr, ok := starlark.AsString(v)
		if !ok {
			return false
		}

		ret = append(ret, fmt.Sprintf("%s=%s", kStr, vStr))

		return true
	})

	return ret
}

// Create implements kati.EnvironmentInterface.
func (p *ShellContext) Create(filename string) (kati.File, error) {
	panic("unimplemented")
}

// NumCPU implements kati.EnvironmentInterface.
func (p *ShellContext) NumCPU() int {
	panic("unimplemented")
}

// Setenv implements kati.EnvironmentInterface.
func (p *ShellContext) Setenv(key string, value string) {
	p.environ.SetKey(starlark.String(key), starlark.String(value))
}

// Unsetenv implements kati.EnvironmentInterface.
func (p *ShellContext) Unsetenv(key string) {
	p.environ.Delete(starlark.String(key))
}

// Exec implements kati.EnvironmentInterface.
func (p *ShellContext) Exec(args []string) ([]byte, error) {
	// slog.Info("ShellContext.Exec", "args", args)

	ret, err := p.runCommand(args, "")
	if err != nil {
		slog.Warn("running a command failed", "err", err)
		return nil, err
	}

	return []byte(ret.Stdout), nil
}

// ReadFile implements kati.EnvironmentInterface.
func (p *ShellContext) ReadFile(filename string) ([]byte, error) {
	slog.Info("ShellContext.ReadFile", "filename", filename)

	val, err := p.getFile(filename)
	if err != nil {
		return nil, err
	}

	contents, ok := starlark.AsString(val)
	if !ok {
		return nil, fmt.Errorf("can not get file contents as string")
	}

	return []byte(contents), nil
}

// Stat implements kati.EnvironmentInterface.
func (p *ShellContext) Stat(filename string) (fs.FileInfo, error) {
	slog.Info("ShellContext.Stat", "filename", filename)

	val, err := p.getFile(filename)
	if err != nil {
		return nil, err
	}

	return &fileStat{name: filename, val: val}, nil
}

// Get implements starlark.HasSetKey.
func (p *ShellContext) Get(k starlark.Value) (v starlark.Value, found bool, err error) {
	return p.files.Get(k)
}

// SetKey implements starlark.HasSetKey.
func (p *ShellContext) SetKey(k starlark.Value, v starlark.Value) error {
	return p.files.SetKey(k, v)
}

func (p *ShellContext) runCommand(args []string, stdin string) (CommandResult, error) {
	cmd, ok := p.commands[args[0]]
	if !ok {
		if p.commandNotFound != nil {
			thread := &starlark.Thread{}

			var argsTuple starlark.Tuple
			for _, arg := range args {
				argsTuple = append(argsTuple, starlark.String(arg))
			}

			ret, err := starlark.Call(thread, p.commandNotFound, starlark.Tuple{p, argsTuple}, []starlark.Tuple{})
			if err != nil {
				return emptyResult(), err
			}

			if ret != starlark.None {
				return emptyResult(), fmt.Errorf("command_not_found result not implemented")
			}
		}

		return emptyResult(), fmt.Errorf("command not found: %s", args[0])
	}

	return cmd.Run(p, args, stdin)
}

func (p *ShellContext) getParam(name string) (string, error) {
	val, ok, err := p.environ.Get(starlark.String(name))
	if err != nil {
		return "", err
	} else if !ok {
		return "", nil
	}

	str, _ := starlark.AsString(val)

	return str, nil
}

func (p *ShellContext) evaluateArithmetic(e syntax.ArithmExpr) (string, error) {
	switch expr := e.(type) {
	case *syntax.BinaryArithm:
		switch {
		case expr.Op == syntax.Sub || expr.Op == syntax.Shl:
			lhs, err := p.evaluateArithmetic(expr.X)
			if err != nil {
				return "", err
			}

			rhs, err := p.evaluateArithmetic(expr.Y)
			if err != nil {
				return "", err
			}

			lhsInt, err := strconv.ParseInt(lhs, 0, 64)
			if err != nil {
				return "", err
			}

			rhsInt, err := strconv.ParseInt(rhs, 0, 64)
			if err != nil {
				return "", err
			}

			switch expr.Op {
			case syntax.Sub:
				return strconv.FormatInt(lhsInt-rhsInt, 10), nil
			case syntax.Shl:
				return strconv.FormatInt(lhsInt<<rhsInt, 10), nil
			default:
				panic("unimplemented")
			}
		default:
			return "", fmt.Errorf("binary op %s not implemented", expr.Op)
		}
	case *syntax.ParenArithm:
		return p.evaluateArithmetic(expr.X)
	case *syntax.Word:
		return p.evaluateWord(expr)
	default:
		return "", fmt.Errorf("word part %T not implemented", expr)
	}
}

func (p *ShellContext) evaluatePart(part syntax.WordPart) (string, error) {
	switch part := part.(type) {
	case *syntax.Lit:
		return part.Value, nil
	case *syntax.ParamExp:
		param, err := p.getParam(part.Param.Value)
		if err != nil {
			return "", err
		}
		return param, nil
	case *syntax.SglQuoted:
		if part.Dollar {
			return "", fmt.Errorf("single quotes with dollar not implemented")
		} else {
			return part.Value, nil
		}
	case *syntax.DblQuoted:
		var ret []string

		// ret = append(ret, "\"")

		for _, part := range part.Parts {
			val, err := p.evaluatePart(part)
			if err != nil {
				return "", err
			}

			ret = append(ret, val)
		}

		// ret = append(ret, "\"")

		return strings.Join(ret, ""), nil
	case *syntax.CmdSubst:
		var ret []string

		for _, stmt := range part.Stmts {
			out, err := p.visitStmt(stmt, "")
			if err != nil {
				return "", err
			}

			ret = append(ret, out.Stdout)
		}

		return strings.Join(ret, ""), nil
	case *syntax.ArithmExp:
		return p.evaluateArithmetic(part.X)
	default:
		return "", fmt.Errorf("word part %T not implemented", part)
	}
}

func (p *ShellContext) evaluateWord(word *syntax.Word) (string, error) {
	var ret []string

	for _, part := range word.Parts {
		val, err := p.evaluatePart(part)
		if err != nil {
			return "", err
		}

		ret = append(ret, val)
	}

	return strings.Join(ret, ""), nil
}

func (p *ShellContext) visitIfClause(cmd *syntax.IfClause, stdin string) (CommandResult, error) {
	if len(cmd.Cond) == 0 {
		for _, stmt := range cmd.Then {
			_, err := p.visitStmt(stmt, "")
			if err != nil {
				return emptyResult(), err
			}
		}

		return emptyResult(), nil
	} else if len(cmd.Cond) == 1 {
		res, err := p.visitStmt(cmd.Cond[0], "")
		if err != nil {
			return emptyResult(), err
		}

		if res.ExitCode == 0 {
			for _, stmt := range cmd.Then {
				_, err := p.visitStmt(stmt, "")
				if err != nil {
					return emptyResult(), err
				}
			}

			return emptyResult(), nil
		} else if cmd.Else != nil {
			return p.visitIfClause(cmd.Else, stdin)
		} else {
			return emptyResult(), nil
		}
	} else {
		return emptyResult(), fmt.Errorf("IfClauses with more than one condition are not implemented")
	}
}

func (p *ShellContext) visitCmd(cmd syntax.Command, stdin string) (CommandResult, error) {
	switch cmd := cmd.(type) {
	case *syntax.CallExpr:
		if len(cmd.Assigns) == 0 {
			var args []string
			for _, arg := range cmd.Args {
				val, err := p.evaluateWord(arg)
				if err != nil {
					return emptyResult(), err
				}
				args = append(args, val)
			}

			return p.runCommand(args, stdin)
		} else {
			for _, assign := range cmd.Assigns {
				k := assign.Name.Value

				if assign.Value != nil {
					val, err := p.evaluateWord(assign.Value)
					if err != nil {
						return emptyResult(), err
					}

					if err := p.environ.SetKey(starlark.String(k), starlark.String(val)); err != nil {
						return emptyResult(), err
					}
				}
			}

			return emptyResult(), nil
		}
	case *syntax.FuncDecl:
		return emptyResult(), fmt.Errorf("FuncDecl not implemented")
	case *syntax.IfClause:
		return p.visitIfClause(cmd, stdin)
	case *syntax.DeclClause:
		switch cmd.Variant.Value {
		case "export":
			for _, assign := range cmd.Args {
				k := assign.Name.Value

				if assign.Value != nil {
					val, err := p.evaluateWord(assign.Value)
					if err != nil {
						return emptyResult(), err
					}

					if err := p.environ.SetKey(starlark.String(k), starlark.String(val)); err != nil {
						return emptyResult(), err
					}
				}
			}

			return emptyResult(), nil
		default:
			return emptyResult(), fmt.Errorf("DeclClause: %s not implemented", cmd.Variant.Value)
		}
	case *syntax.BinaryCmd:
		switch cmd.Op {
		case syntax.Pipe:
			lhs, err := p.visitStmt(cmd.X, "")
			if err != nil {
				return emptyResult(), err
			}

			rhs, err := p.visitStmt(cmd.Y, lhs.Stdout)
			if err != nil {
				return emptyResult(), err
			}

			return rhs, nil
		case syntax.AndStmt:
			lhs, err := p.visitStmt(cmd.X, "")
			if err != nil {
				return emptyResult(), err
			}

			if lhs.ExitCode == 0 {
				rhs, err := p.visitStmt(cmd.Y, "")
				if err != nil {
					return emptyResult(), err
				}

				return rhs, nil
			}

			return lhs, nil
		default:
			return emptyResult(), fmt.Errorf("BinaryCmd op %s not implemented", cmd.Op.String())
		}
	default:
		return emptyResult(), fmt.Errorf("command %T not implemented", cmd)
	}
}

func (p *ShellContext) visitStmt(stmt *syntax.Stmt, stdin string) (CommandResult, error) {
	res, err := p.visitCmd(stmt.Cmd, stdin)
	if err != nil {
		return emptyResult(), err
	}

	return res, nil
}

func (t *ShellContext) String() string { return "ShellContext" }
func (*ShellContext) Type() string     { return "ShellContext" }
func (*ShellContext) Hash() (uint32, error) {
	return 0, fmt.Errorf("ShellContext is not hashable")
}
func (*ShellContext) Truth() starlark.Bool { return starlark.True }
func (*ShellContext) Freeze()              {}

// Attr implements starlark.HasAttrs.
func (p *ShellContext) Attr(name string) (starlark.Value, error) {
	if name == "eval" {
		return starlark.NewBuiltin("Shell.eval", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				contents     string
				returnStdout bool
			)

			if err := starlark.UnpackArgs("Shell.eval", args, kwargs,
				"contents", &contents,
				"return_stdout?", &returnStdout,
			); err != nil {
				return starlark.None, err
			}

			parser := syntax.NewParser()

			f, err := parser.Parse(bytes.NewReader([]byte(contents)), "")
			if err != nil {
				return nil, err
			}

			stdout := ""

			for _, stmt := range f.Stmts {
				stmtResult, err := p.visitStmt(stmt, "")
				if err != nil {
					return nil, err
				}

				stdout += stmtResult.Stdout
			}

			if returnStdout {
				return starlark.String(stdout), nil
			} else {
				return p.environ, nil
			}
		}), nil
	} else if name == "eval_makefile" {
		return starlark.NewBuiltin("Shell.eval_makefile", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				argsV *starlark.List

				// Pass user errors as a string.
				returnErrors bool
			)

			if err := starlark.UnpackArgs("Shell.eval_makefile", args, kwargs,
				"args", &argsV,
				"return_errors?", &returnErrors,
			); err != nil {
				return starlark.None, err
			}

			var argsS []string
			argsV.Elements(func(v starlark.Value) bool {
				str, ok := starlark.AsString(v)
				if !ok {
					return false
				}

				argsS = append(argsS, str)

				return true
			})

			loadReq, err := kati.FromCommandLine(p, argsS)
			if err != nil {
				return starlark.None, err
			}

			loadReq.EnvironmentVars = p.Environ()

			depGraph, err := kati.Load(p, loadReq)
			if err != nil {
				if returnErrors {
					return starlark.String(err.Error()), nil
				} else {
					return starlark.None, err
				}
			}

			return &starDepGraph{graph: depGraph}, nil
		}), nil
	} else if name == "add_command" {
		return starlark.NewBuiltin("Shell.add_command", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				name string
				f    *starlark.Function
			)

			if err := starlark.UnpackArgs("Shell.add_command", args, kwargs,
				"name", &name,
				"f", &f,
			); err != nil {
				return starlark.None, err
			}

			p.commands[name] = &shellCommand{name: name, f: f}

			return starlark.None, nil
		}), nil
	} else if name == "set_environment" {
		return starlark.NewBuiltin("Shell.set_environment", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				key   string
				value string
			)

			if err := starlark.UnpackArgs("Shell.eval", args, kwargs,
				"key", &key,
				"value", &value,
			); err != nil {
				return starlark.None, err
			}

			if err := p.environ.SetKey(starlark.String(key), starlark.String(value)); err != nil {
				return starlark.None, err
			}

			return starlark.None, nil
		}), nil
	} else if name == "set_handlers" {
		return starlark.NewBuiltin("Shell.set_handlers", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				fileNotFound    *starlark.Function
				commandNotFound *starlark.Function
			)

			if err := starlark.UnpackArgs("Shell.set_handlers", args, kwargs,
				"file_not_found", &fileNotFound,
				"command_not_found", &commandNotFound,
			); err != nil {
				return starlark.None, err
			}

			if fileNotFound != nil {
				p.fileNotFound = fileNotFound
			}

			if commandNotFound != nil {
				p.commandNotFound = commandNotFound
			}

			return starlark.None, nil
		}), nil
	} else if name == "move" {
		return starlark.NewBuiltin("Shell.move", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				src string
				dst string
			)

			if err := starlark.UnpackArgs("Shell.move", args, kwargs,
				"src", &src,
				"dst", &dst,
			); err != nil {
				return starlark.None, err
			}

			srcFile, exists, err := p.files.Delete(starlark.String(src))
			if err != nil {
				return starlark.None, err
			}

			if !exists {
				return starlark.None, fmt.Errorf("file not found: %s", src)
			}

			if err := p.files.SetKey(starlark.String(dst), srcFile); err != nil {
				return starlark.None, err
			}

			return starlark.None, nil
		}), nil
	} else if name == "env" {
		return p.environ, nil
	} else if name == "state" {
		return p.state, nil
	} else if name == "stdin" {
		return starlark.String(p.stdin), nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (p *ShellContext) AttrNames() []string {
	return []string{"eval", "eval_makefile", "add_command", "set_environment", "set_handlers", "move", "env", "state"}
}

var (
	_ starlark.Value            = &ShellContext{}
	_ starlark.HasAttrs         = &ShellContext{}
	_ starlark.HasSetKey        = &ShellContext{}
	_ kati.EnvironmentInterface = &ShellContext{}
)

package main

import (
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/bazelbuild/buildtools/build"
	"github.com/lmittmann/tint"
	"github.com/mattn/go-isatty"
	"mvdan.cc/sh/v3/syntax"
)

// Grab scripts with: ./tools/build.go -run -- build scripts/get_scripts.star:script_fs -o local/scripts.tar
// Test with: find local/scripts | grep "\.sh" | xargs go run github.com/tinyrange/tinyrange/experimental/shToStar

func randomId() string {
	var val [4]byte
	_, err := rand.Read(val[:])
	if err != nil {
		panic(err)
	}
	return "v" + hex.EncodeToString(val[:])
}

type block interface {
	Add(expr build.Expr) error
}

type functionBlock struct {
	*build.DefStmt
}

// Add implements block.
func (f *functionBlock) Add(expr build.Expr) error {
	f.Body = append(f.Body, expr)

	return nil
}

type fileBlock struct {
	*build.File
}

// Add implements block.
func (f *fileBlock) Add(expr build.Expr) error {
	f.Stmt = append(f.Stmt, expr)

	return nil
}

type ifBlock struct {
	*build.IfStmt
}

// Add implements block.
func (i *ifBlock) Add(expr build.Expr) error {
	i.True = append(i.True, expr)

	return nil
}

var (
	_ block = &functionBlock{}
	_ block = &fileBlock{}
	_ block = &ifBlock{}
)

type ShellScriptToStarlark struct {
	file     *build.File
	mainFunc *build.DefStmt
}

func (sh *ShellScriptToStarlark) declareVariable(target block, value build.Expr) (string, error) {
	name := randomId()

	return name, target.Add(&build.AssignExpr{
		LHS: &build.Ident{Name: name},
		Op:  "=",
		RHS: value,
	})
}

func (sh *ShellScriptToStarlark) declareFunction(cb func(name string, target block) error) (string, error) {
	name := randomId()

	newFunc := &build.DefStmt{
		Function: build.Function{
			Params: []build.Expr{
				&build.Ident{Name: "ctx"},
			},
		},
		Name: name,
	}

	if err := cb(name, &functionBlock{DefStmt: newFunc}); err != nil {
		return "", err
	}

	target := &fileBlock{File: sh.file}

	return name, target.Add(newFunc)
}

func (sh *ShellScriptToStarlark) getBuiltin(val build.Expr) build.Expr {
	str, ok := val.(*build.StringExpr)
	if !ok {
		return nil
	}

	switch str.Value {
	case "exit":
		return &build.DotExpr{
			X:    &build.Ident{Name: "builtin"},
			Name: "exit",
		}
	case "exec":
		return &build.DotExpr{
			X:    &build.Ident{Name: "builtin"},
			Name: "exec",
		}
	case ":":
		return &build.DotExpr{
			X:    &build.Ident{Name: "builtin"},
			Name: "noop",
		}
	case "cat":
		return &build.DotExpr{
			X:    &build.Ident{Name: "builtin"},
			Name: "cat",
		}
	case "umask":
		return &build.DotExpr{
			X:    &build.Ident{Name: "builtin"},
			Name: "umask",
		}
	case "[":
		return &build.DotExpr{
			X:    &build.Ident{Name: "builtin"},
			Name: "compare",
		}
	case "continue":
		return &build.DotExpr{
			X:    &build.Ident{Name: "builtin"},
			Name: "continue",
		}
	default:
		return nil
	}
}

func (sh *ShellScriptToStarlark) translatePart(target block, part syntax.WordPart) (build.Expr, error) {
	switch part := part.(type) {
	case *syntax.Lit:
		return &build.StringExpr{Value: part.Value}, nil
	case *syntax.DblQuoted:
		if part.Dollar {
			return nil, fmt.Errorf("part.Dollar not implemented")
		}

		var parts []build.Expr

		for _, child := range part.Parts {
			childExpr, err := sh.translatePart(target, child)
			if err != nil {
				return nil, err
			}

			parts = append(parts, childExpr)
		}

		if len(parts) > 1 {
			return nil, fmt.Errorf("len(parts) > 1")
		}

		return parts[0], nil
	case *syntax.CmdSubst:
		name, err := sh.declareFunction(func(name string, target block) error {
			for _, stmt := range part.Stmts {
				expr, isExpr, err := sh.translateStmt(target, stmt)
				if err != nil {
					return err
				}

				if isExpr {
					expr = &build.CallExpr{
						X: &build.DotExpr{
							X:    expr,
							Name: "run",
						},
						List: []build.Expr{
							&build.Ident{Name: "ctx"},
						},
					}
				}

				if err := target.Add(expr); err != nil {
					return err
				}
			}

			return nil
		})
		if err != nil {
			return nil, err
		}

		return &build.CallExpr{
			X: &build.DotExpr{
				X:    &build.Ident{Name: "ctx"},
				Name: "subshell",
			},
			List: []build.Expr{
				&build.Ident{Name: name},
			},
		}, nil
	case *syntax.ParamExp:
		if part.Excl {
			return nil, fmt.Errorf("part.Excl not implemented")
		}
		if part.Length {
			return nil, fmt.Errorf("part.Length not implemented")
		}
		if part.Width {
			return nil, fmt.Errorf("part.Width != nil")
		}
		if part.Index != nil {
			return nil, fmt.Errorf("part.Index != nil")
		}
		if part.Slice != nil {
			return nil, fmt.Errorf("part.Slice != nil")
		}
		if part.Repl != nil {
			return nil, fmt.Errorf("part.Repl != nil")
		}
		if part.Exp != nil {
			return nil, fmt.Errorf("part.Exp != nil")
		}

		return &build.CallExpr{X: &build.DotExpr{
			X:    &build.Ident{Name: "ctx"},
			Name: "get",
		}, List: []build.Expr{
			&build.StringExpr{Value: part.Param.Value},
		}}, nil
	default:
		return nil, fmt.Errorf("translatePart not implemented: %T %+v", part, part)
	}
}

func (sh *ShellScriptToStarlark) translateWord(target block, word *syntax.Word) (build.Expr, error) {
	var ret []build.Expr

	for _, part := range word.Parts {
		expr, err := sh.translatePart(target, part)
		if err != nil {
			return nil, err
		}

		ret = append(ret, expr)
	}

	if len(ret) > 1 {
		return &build.CallExpr{
			X:    &build.Ident{Name: "join"},
			List: ret,
		}, nil
	} else {
		return ret[0], nil
	}
}

func (sh *ShellScriptToStarlark) translateCmd(target block, cmd syntax.Command) (build.Expr, bool, error) {
	switch cmd := cmd.(type) {
	case *syntax.CallExpr:
		var args []build.Expr

		var ret build.Expr

		for _, word := range cmd.Args {
			arg, err := sh.translateWord(target, word)
			if err != nil {
				return nil, false, err
			}

			args = append(args, arg)
		}

		if len(args) > 0 {
			builtin := sh.getBuiltin(args[0])

			if builtin != nil {
				ret = &build.CallExpr{
					X:    builtin,
					List: args[1:],
				}
			} else {
				ret = &build.CallExpr{
					X:    &build.Ident{Name: "call"},
					List: args,
				}
			}
		}

		if len(cmd.Assigns) > 0 {
			vals := []*build.KeyValueExpr{}

			for _, assign := range cmd.Assigns {
				if assign.Append {
					return nil, false, fmt.Errorf("assign.Append not implemented")
				}
				if assign.Naked {
					return nil, false, fmt.Errorf("assign.Naked not implemented")
				}
				if assign.Index != nil {
					return nil, false, fmt.Errorf("assign.Index not implemented")
				}
				if assign.Array != nil {
					return nil, false, fmt.Errorf("assign.Array not implemented")
				}

				var val build.Expr

				if assign.Value == nil {
					val = &build.StringExpr{Value: ""}
				} else {
					var err error

					val, err = sh.translateWord(target, assign.Value)
					if err != nil {
						return nil, false, err
					}
				}

				vals = append(vals, &build.KeyValueExpr{
					Key:   &build.StringExpr{Value: assign.Name.Value},
					Value: val,
				})
			}

			if ret == nil {
				ret = &build.CallExpr{
					X: &build.DotExpr{
						X:    &build.Ident{Name: "ctx"},
						Name: "set_environment",
					},
					List: []build.Expr{
						&build.DictExpr{
							List: vals,
						},
					},
				}
			} else {
				ret = &build.CallExpr{
					X: &build.DotExpr{
						X:    ret,
						Name: "with_environment",
					},
					List: []build.Expr{
						&build.DictExpr{
							List: vals,
						},
					},
				}
			}
		}

		return ret, true, nil
	case *syntax.CaseClause:
		word, err := sh.translateWord(target, cmd.Word)
		if err != nil {
			return nil, false, err
		}

		name, err := sh.declareVariable(target, word)
		if err != nil {
			return nil, false, err
		}

		_ = name

		for _, c := range cmd.Items {
			switch c.Op {
			case syntax.Break:
				var checks []build.Expr

				for _, pattern := range c.Patterns {
					check, err := sh.translateWord(target, pattern)
					if err != nil {
						return nil, false, err
					}

					checks = append(checks, check)
				}

				ifStmt := &build.IfStmt{
					Cond: &build.CallExpr{
						X:    &build.Ident{Name: "compare"},
						List: append([]build.Expr{&build.Ident{Name: name}}, checks...),
					},
				}

				ifTarget := &ifBlock{IfStmt: ifStmt}

				for _, stmt := range c.Stmts {
					expr, isExpr, err := sh.translateStmt(target, stmt)
					if err != nil {
						return nil, false, err
					}

					if isExpr {
						expr = &build.CallExpr{
							X: &build.DotExpr{
								X:    expr,
								Name: "run",
							},
							List: []build.Expr{
								&build.Ident{Name: "ctx"},
							},
						}
					}

					if err := ifTarget.Add(expr); err != nil {
						return nil, false, err
					}
				}

				if err := target.Add(ifStmt); err != nil {
					return nil, false, err
				}
			default:
				return nil, false, fmt.Errorf("cmd.Items.Op not implemented: %s", c.Op)
			}
		}

		return nil, false, nil
	case *syntax.BinaryCmd:
		switch cmd.Op {
		case syntax.OrStmt:
			lhs, isExpr, err := sh.translateStmt(target, cmd.X)
			if err != nil {
				return nil, false, err
			}
			if !isExpr {
				return nil, false, fmt.Errorf("BinaryCmd lhs is not a expression")
			}

			rhs, isExpr, err := sh.translateStmt(target, cmd.Y)
			if err != nil {
				return nil, false, err
			}
			if !isExpr {
				return nil, false, fmt.Errorf("BinaryCmd rhs is not a expression")
			}

			return &build.CallExpr{
				X: &build.DotExpr{
					X:    lhs,
					Name: "or",
				},
				List: []build.Expr{rhs},
			}, true, nil
		case syntax.AndStmt:
			lhs, isExpr, err := sh.translateStmt(target, cmd.X)
			if err != nil {
				return nil, false, err
			}
			if !isExpr {
				return nil, false, fmt.Errorf("BinaryCmd lhs is not a expression")
			}

			rhs, isExpr, err := sh.translateStmt(target, cmd.Y)
			if err != nil {
				return nil, false, err
			}
			if !isExpr {
				return nil, false, fmt.Errorf("BinaryCmd rhs is not a expression")
			}

			return &build.CallExpr{
				X: &build.DotExpr{
					X:    lhs,
					Name: "and",
				},
				List: []build.Expr{rhs},
			}, true, nil
		default:
			return nil, false, fmt.Errorf("*syntax.BinaryCmd.Op not implemented: %s", cmd.Op)
		}
	case *syntax.ForClause:
		if cmd.Select {
			return nil, false, fmt.Errorf("cmd.Select not implemented")
		}

		name, err := sh.declareFunction(func(name string, target block) error {
			for _, stmt := range cmd.Do {
				expr, isExpr, err := sh.translateStmt(target, stmt)
				if err != nil {
					return err
				}

				if isExpr {
					expr = &build.CallExpr{
						X: &build.DotExpr{
							X:    expr,
							Name: "run",
						},
						List: []build.Expr{
							&build.Ident{Name: "ctx"},
						},
					}
				}

				if err := target.Add(expr); err != nil {
					return err
				}
			}

			return nil
		})
		if err != nil {
			return nil, false, err
		}

		switch loop := cmd.Loop.(type) {
		case *syntax.WordIter:
			var args []build.Expr

			for _, arg := range loop.Items {
				expr, err := sh.translateWord(target, arg)
				if err != nil {
					return nil, false, err
				}

				args = append(args, expr)
			}

			return &build.CallExpr{
				X: &build.DotExpr{
					X:    &build.Ident{Name: "ctx"},
					Name: "for_range",
				},
				List: append(
					[]build.Expr{
						&build.Ident{Name: name},
						&build.StringExpr{Value: loop.Name.Value},
					},
					args...,
				),
			}, false, nil
		default:
			return nil, false, fmt.Errorf("*syntax.ForClause loop not implemented: %T %+v", loop, loop)
		}
	case *syntax.IfClause:
		check, err := sh.declareFunction(func(name string, target block) error {
			for _, stmt := range cmd.Cond {
				expr, isExpr, err := sh.translateStmt(target, stmt)
				if err != nil {
					return err
				}

				if isExpr {
					expr = &build.CallExpr{
						X: &build.DotExpr{
							X:    expr,
							Name: "run",
						},
						List: []build.Expr{
							&build.Ident{Name: "ctx"},
						},
					}
				}

				if err := target.Add(expr); err != nil {
					return err
				}
			}

			return nil
		})
		if err != nil {
			return nil, false, err
		}

		ifStmt := &build.IfStmt{
			Cond: &build.CallExpr{
				X: &build.Ident{Name: "check_subshell"},
				List: []build.Expr{
					&build.Ident{Name: check},
				},
			},
		}

		ifTarget := &ifBlock{IfStmt: ifStmt}

		for _, stmt := range cmd.Then {
			expr, isExpr, err := sh.translateStmt(target, stmt)
			if err != nil {
				return nil, false, err
			}

			if isExpr {
				expr = &build.CallExpr{
					X: &build.DotExpr{
						X:    expr,
						Name: "run",
					},
					List: []build.Expr{
						&build.Ident{Name: "ctx"},
					},
				}
			}

			if err := ifTarget.Add(expr); err != nil {
				return nil, false, err
			}
		}

		if err := target.Add(ifStmt); err != nil {
			return nil, false, err
		}

		return nil, false, nil
	default:
		return nil, false, fmt.Errorf("translateCmd not implemented: %T %+v", cmd, cmd)
	}
}

func (sh *ShellScriptToStarlark) translateStmt(target block, stmt *syntax.Stmt) (build.Expr, bool, error) {
	expr, isExpr, err := sh.translateCmd(target, stmt.Cmd)
	if err != nil {
		return nil, false, err
	}

	if !isExpr {
		return expr, false, nil
	}

	top := expr

	for _, redir := range stmt.Redirs {
		var fd build.Expr = &build.LiteralExpr{
			Token: "0",
		}

		if redir.N != nil {
			fd = &build.LiteralExpr{
				Token: redir.N.Value,
			}
		}

		var hdoc build.Expr

		if redir.Hdoc != nil {
			hdoc, err = sh.translateWord(target, redir.Hdoc)
			if err != nil {
				return nil, false, err
			}
		}

		_ = hdoc

		switch redir.Op {
		case syntax.RdrOut:
			redirectTo, err := sh.translateWord(target, redir.Word)
			if err != nil {
				return nil, false, err
			}

			top = &build.CallExpr{
				X: &build.DotExpr{
					X:    top,
					Name: "redirect",
				},
				List: []build.Expr{fd, redirectTo},
			}
		case syntax.RdrAll:
			redirectTo, err := sh.translateWord(target, redir.Word)
			if err != nil {
				return nil, false, err
			}

			top = &build.CallExpr{
				X: &build.DotExpr{
					X:    top,
					Name: "redirect_all",
				},
				List: []build.Expr{redirectTo},
			}
		case syntax.DplOut:
			redirectTo, err := sh.translateWord(target, redir.Word)
			if err != nil {
				return nil, false, err
			}

			top = &build.CallExpr{
				X: &build.DotExpr{
					X:    top,
					Name: "duplicate_out",
				},
				List: []build.Expr{redirectTo},
			}
		case syntax.Hdoc:
			top = &build.CallExpr{
				X: &build.DotExpr{
					X:    top,
					Name: "pipe_stdin",
				},
				List: []build.Expr{hdoc},
			}
		default:
			return nil, false, fmt.Errorf("stmt.Redirs.Op not implemented: %s", redir.Op)
		}
	}

	if stmt.Background {
		top = &build.CallExpr{
			X: &build.DotExpr{
				X:    top,
				Name: "background",
			},
		}
	}

	if stmt.Coprocess {
		return nil, false, fmt.Errorf("stmt.Coprocess not implemented")
	}

	if stmt.Negated {
		top = &build.CallExpr{
			X: &build.DotExpr{
				X:    top,
				Name: "negated",
			},
		}
	}

	return top, true, nil
}

func (sh *ShellScriptToStarlark) translateFile(f *syntax.File) error {
	target := &functionBlock{DefStmt: sh.mainFunc}

	for _, stmt := range f.Stmts {
		top, isExpr, err := sh.translateStmt(target, stmt)
		if err != nil {
			return err
		}

		if isExpr {
			top = &build.CallExpr{
				X: &build.DotExpr{
					X:    top,
					Name: "run",
				},
				List: []build.Expr{
					&build.Ident{Name: "ctx"},
				},
			}
		}

		if err := target.Add(top); err != nil {
			return err
		}
	}

	return nil
}

func (sh *ShellScriptToStarlark) emit() []byte {
	return build.Format(sh.file)
}

func (sh *ShellScriptToStarlark) TranslateFile(r io.Reader, filename string, out io.Writer) ([]byte, error) {
	parser := syntax.NewParser()

	f, err := parser.Parse(r, filename)
	if err != nil {
		return nil, err
	}

	if err := sh.translateFile(f); err != nil {
		return nil, err
	}

	sh.file.Stmt = append(sh.file.Stmt, sh.mainFunc)

	return sh.emit(), nil
}

func translateFile(input string) error {
	f, err := os.Open(input)
	if err != nil {
		return err
	}
	defer f.Close()

	sh := &ShellScriptToStarlark{
		file: &build.File{
			Type: build.TypeDefault,
		},
		mainFunc: &build.DefStmt{
			Function: build.Function{
				Params: []build.Expr{
					&build.Ident{Name: "ctx"},
				},
			},
			Name: "main",
		},
	}

	out, err := sh.TranslateFile(f, input, os.Stdout)
	if err != nil {
		return err
	}

	if _, err := os.Stdout.Write(out); err != nil {
		return err
	}

	return nil
}

func appMain() error {
	flag.Parse()

	w := os.Stderr

	slog.SetDefault(slog.New(
		tint.NewHandler(w, &tint.Options{
			Level:      slog.LevelDebug,
			TimeFormat: time.RFC3339Nano,
			NoColor:    !isatty.IsTerminal(w.Fd()),
		}),
	))

	for _, input := range flag.Args() {
		slog.Info("", "in", input)
		if err := translateFile(input); err != nil {
			slog.Error("error translating", "in", input, "err", err)
		}
	}

	return nil
}

func main() {
	if err := appMain(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

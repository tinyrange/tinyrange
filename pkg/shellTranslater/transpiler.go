package shelltranslater

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"

	"github.com/bazelbuild/buildtools/build"
	"mvdan.cc/sh/v3/syntax"
)

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
	if expr == nil {
		panic("expr == nil")
	}

	f.Body = append(f.Body, expr)

	return nil
}

type fileBlock struct {
	*build.File
}

// Add implements block.
func (f *fileBlock) Add(expr build.Expr) error {
	if expr == nil {
		panic("expr == nil")
	}

	f.Stmt = append(f.Stmt, expr)

	return nil
}

type ifBlock struct {
	*build.IfStmt
}

// Add implements block.
func (i *ifBlock) Add(expr build.Expr) error {
	if expr == nil {
		panic("expr == nil")
	}

	i.True = append(i.True, expr)

	return nil
}

var (
	_ block = &functionBlock{}
	_ block = &fileBlock{}
	_ block = &ifBlock{}
)

type ShellScriptToStarlark struct {
	includeExternalScripts bool
	debianMode             bool
	file                   *build.File
	mainFunc               *build.DefStmt
}

func (sh *ShellScriptToStarlark) declareVariable(target block, value build.Expr) (string, error) {
	name := randomId()

	return name, target.Add(&build.AssignExpr{
		LHS: &build.Ident{Name: name},
		Op:  "=",
		RHS: value,
	})
}

func (sh *ShellScriptToStarlark) declareFunction(name string, cb func(name string, target block) error) (string, error) {
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

var sourceBuiltin = &build.Ident{Name: "source"}

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
	case "readlink":
		return &build.DotExpr{
			X:    &build.Ident{Name: "builtin"},
			Name: "readlink",
		}
	case "echo":
		return &build.DotExpr{
			X:    &build.Ident{Name: "builtin"},
			Name: "echo",
		}
	case "umask":
		return &build.DotExpr{
			X:    &build.Ident{Name: "builtin"},
			Name: "umask",
		}
	case "[":
		return &build.DotExpr{
			X:    &build.Ident{Name: "builtin"},
			Name: "test",
		}
	case "continue":
		return &build.DotExpr{
			X:    &build.Ident{Name: "builtin"},
			Name: "flow_continue",
		}
	case "return":
		return &build.DotExpr{
			X:    &build.Ident{Name: "builtin"},
			Name: "flow_return",
		}
	case "set":
		return &build.DotExpr{
			X:    &build.Ident{Name: "builtin"},
			Name: "set",
		}
	case "eval":
		return &build.DotExpr{
			X:    &build.Ident{Name: "builtin"},
			Name: "eval",
		}
	case "command":
		return &build.DotExpr{
			X:    &build.Ident{Name: "builtin"},
			Name: "command",
		}
	case "true":
		return &build.DotExpr{
			X:    &build.Ident{Name: "builtin"},
			Name: "true",
		}
	case "which":
		return &build.DotExpr{
			X:    &build.Ident{Name: "builtin"},
			Name: "which",
		}
	case "cd":
		return &build.DotExpr{
			X:    &build.Ident{Name: "builtin"},
			Name: "cd",
		}
	case ".":
		return sourceBuiltin
	case "source":
		return sourceBuiltin
	}

	if !sh.debianMode {
		return nil
	}

	switch str.Value {
	case "dpkg-maintscript-helper":
		return &build.DotExpr{
			X:    &build.Ident{Name: "debian"},
			Name: "dpkg_maintscript_helper",
		}
	case "deb-systemd-helper":
		return &build.DotExpr{
			X:    &build.Ident{Name: "debian"},
			Name: "deb_systemd_helper",
		}
	case "update-rc.d":
		return &build.DotExpr{
			X:    &build.Ident{Name: "debian"},
			Name: "update_rc_d",
		}
	case "invoke-rc.d":
		return &build.DotExpr{
			X:    &build.Ident{Name: "debian"},
			Name: "invoke_rc_d",
		}
	case "py3compile":
		return &build.DotExpr{
			X:    &build.Ident{Name: "debian"},
			Name: "py3compile",
		}
	case "update-alternatives":
		return &build.DotExpr{
			X:    &build.Ident{Name: "debian"},
			Name: "update_alternatives",
		}
	}

	return nil
}

func (sh *ShellScriptToStarlark) translatePart(target block, part syntax.WordPart) (build.Expr, error) {
	switch part := part.(type) {
	case *syntax.Lit:
		return &build.StringExpr{Value: part.Value}, nil
	case *syntax.SglQuoted:
		if part.Dollar {
			return nil, fmt.Errorf("part.Dollar not implemented")
		}

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

		if len(parts) == 0 {
			return &build.StringExpr{Value: ""}, nil
		}

		if len(parts) > 1 {
			return &build.CallExpr{
				X: &build.DotExpr{
					X:    &build.Ident{Name: "ctx"},
					Name: "join",
				},
				List: parts,
			}, nil
		} else {
			return parts[0], nil
		}
	case *syntax.CmdSubst:
		name, err := sh.declareFunction(randomId(), func(name string, target block) error {
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
			switch part.Exp.Op {
			case syntax.DefaultUnsetOrNull:
				var def build.Expr = &build.StringExpr{Value: ""}

				if part.Exp.Word != nil {
					var err error
					def, err = sh.translateWord(target, part.Exp.Word)
					if err != nil {
						return nil, err
					}
				}

				return &build.CallExpr{X: &build.DotExpr{
					X:    &build.Ident{Name: "ctx"},
					Name: "variable",
				}, List: []build.Expr{
					&build.StringExpr{Value: part.Param.Value},
					def,
				}}, nil
			case syntax.RemSmallPrefix:
				var def build.Expr = &build.StringExpr{Value: ""}

				if part.Exp.Word != nil {
					var err error
					def, err = sh.translateWord(target, part.Exp.Word)
					if err != nil {
						return nil, err
					}
				}

				return &build.CallExpr{X: &build.DotExpr{
					X:    &build.Ident{Name: "ctx"},
					Name: "variable_remove_prefix",
				}, List: []build.Expr{
					&build.StringExpr{Value: part.Param.Value},
					def,
					&build.Ident{Name: "False"},
				}}, nil
			case syntax.RemLargePrefix:
				var def build.Expr = &build.StringExpr{Value: ""}

				if part.Exp.Word != nil {
					var err error
					def, err = sh.translateWord(target, part.Exp.Word)
					if err != nil {
						return nil, err
					}
				}

				return &build.CallExpr{X: &build.DotExpr{
					X:    &build.Ident{Name: "ctx"},
					Name: "variable_remove_prefix",
				}, List: []build.Expr{
					&build.StringExpr{Value: part.Param.Value},
					def,
					&build.Ident{Name: "True"},
				}}, nil
			default:
				return nil, fmt.Errorf("part.Exp.Op %s not implemented", part.Exp.Op)
			}
		} else {
			return &build.CallExpr{X: &build.DotExpr{
				X:    &build.Ident{Name: "ctx"},
				Name: "variable",
			}, List: []build.Expr{
				&build.StringExpr{Value: part.Param.Value},
			}}, nil
		}
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
			X: &build.DotExpr{
				X:    &build.Ident{Name: "ctx"},
				Name: "join",
			},
			List: ret,
		}, nil
	} else {
		return ret[0], nil
	}
}

func asString(expr build.Expr) (string, error) {
	switch expr := expr.(type) {
	case *build.StringExpr:
		return expr.Value, nil
	default:
		return "", fmt.Errorf("asString not implemented: %T %+v", expr, expr)
	}
}

func (sh *ShellScriptToStarlark) sourceFile(target block, expr build.Expr) error {
	filename, err := asString(expr)
	if err != nil {
		return err
	}

	if !sh.includeExternalScripts {
		return fmt.Errorf("sourceFile only supported if includeExternalScripts is set")
	}

	r, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer r.Close()

	parser := syntax.NewParser()

	f, err := parser.Parse(r, filename)
	if err != nil {
		return err
	}

	if err := sh.translateFile(target, f); err != nil {
		return err
	}

	return nil
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

			if builtin == sourceBuiltin {
				if err := sh.sourceFile(target, args[1]); err != nil {
					return nil, false, err
				}

				return nil, false, nil
			} else if builtin != nil {
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
				return &build.CallExpr{
					X: &build.DotExpr{
						X:    &build.Ident{Name: "ctx"},
						Name: "set",
					},
					List: []build.Expr{
						&build.DictExpr{
							List: vals,
						},
					},
				}, false, nil
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

		var last *build.IfStmt

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
						X: &build.DotExpr{
							X:    &build.Ident{Name: "ctx"},
							Name: "compare",
						},
						List: append([]build.Expr{&build.Ident{Name: name}}, checks...),
					},
				}

				ifTarget := &ifBlock{IfStmt: ifStmt}

				for _, stmt := range c.Stmts {
					expr, isExpr, err := sh.translateStmt(target, stmt)
					if err != nil {
						return nil, false, err
					}

					if expr != nil {
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
				}

				if len(ifTarget.True) == 0 {
					if err := ifTarget.Add(&build.Ident{Name: "pass"}); err != nil {
						return nil, false, err
					}
				}

				if last != nil {
					last.False = append(last.False, ifStmt)

					last = ifStmt
				} else {
					if err := target.Add(ifStmt); err != nil {
						return nil, false, err
					}

					last = ifStmt
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
					Name: "cmp_or",
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
					Name: "cmp_and",
				},
				List: []build.Expr{rhs},
			}, true, nil
		// case syntax.Pipe:
		// 	lhs, isExpr, err := sh.translateStmt(target, cmd.X)
		// 	if err != nil {
		// 		return nil, false, err
		// 	}
		// 	if !isExpr {
		// 		return nil, false, fmt.Errorf("BinaryCmd lhs is not a expression")
		// 	}

		// 	rhs, isExpr, err := sh.translateStmt(target, cmd.Y)
		// 	if err != nil {
		// 		return nil, false, err
		// 	}
		// 	if !isExpr {
		// 		return nil, false, fmt.Errorf("BinaryCmd rhs is not a expression")
		// 	}

		// 	return &build.CallExpr{
		// 		X: &build.DotExpr{
		// 			X:    lhs,
		// 			Name: "pipe",
		// 		},
		// 		List: []build.Expr{rhs},
		// 	}, true, nil
		default:
			return nil, false, fmt.Errorf("*syntax.BinaryCmd.Op not implemented: %s", cmd.Op)
		}
	case *syntax.ForClause:
		if cmd.Select {
			return nil, false, fmt.Errorf("cmd.Select not implemented")
		}

		name, err := sh.declareFunction(randomId(), func(name string, target block) error {
			for _, stmt := range cmd.Do {
				expr, isExpr, err := sh.translateStmt(target, stmt)
				if err != nil {
					return err
				}

				if expr != nil {
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
		check, err := sh.declareFunction(randomId(), func(name string, target block) error {
			for _, stmt := range cmd.Cond {
				expr, isExpr, err := sh.translateStmt(target, stmt)
				if err != nil {
					return err
				}

				if expr != nil {
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
			}

			return nil
		})
		if err != nil {
			return nil, false, err
		}

		ifStmt := &build.IfStmt{
			Cond: &build.CallExpr{
				X: &build.DotExpr{
					X:    &build.Ident{Name: "ctx"},
					Name: "check_subshell",
				},
				List: []build.Expr{
					&build.Ident{Name: check},
				},
			},
		}

		ifTarget := &ifBlock{IfStmt: ifStmt}

		for _, stmt := range cmd.Then {
			expr, isExpr, err := sh.translateStmt(ifTarget, stmt)
			if err != nil {
				return nil, false, err
			}

			if expr != nil {
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
		}

		if len(ifTarget.True) == 0 {
			if err := ifTarget.Add(&build.Ident{Name: "pass"}); err != nil {
				return nil, false, err
			}
		}

		if err := target.Add(ifStmt); err != nil {
			return nil, false, err
		}

		return nil, false, nil
	case *syntax.FuncDecl:
		// if cmd.RsrvWord {
		// 	return nil, false, fmt.Errorf("cmd.RsrvWord not implemented")
		// }
		// if cmd.Parens {
		// 	return nil, false, fmt.Errorf("cmd.Parens not implemented")
		// }

		_, err := sh.declareFunction(cmd.Name.Value, func(name string, target block) error {
			body := []*syntax.Stmt{cmd.Body}

			if block, ok := cmd.Body.Cmd.(*syntax.Block); ok {
				body = block.Stmts
			}

			for _, stmt := range body {
				expr, isExpr, err := sh.translateStmt(target, stmt)
				if err != nil {
					return err
				}

				if expr != nil {
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
			}

			return nil
		})
		if err != nil {
			return nil, false, err
		}

		return nil, false, nil
	case *syntax.Block:
		name, err := sh.declareFunction(randomId(), func(name string, target block) error {
			for _, stmt := range cmd.Stmts {
				expr, isExpr, err := sh.translateStmt(target, stmt)
				if err != nil {
					return err
				}

				if expr != nil {
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
			}

			return nil
		})
		if err != nil {
			return nil, false, err
		}

		return &build.CallExpr{
			X: &build.DotExpr{
				X:    &build.Ident{Name: "ctx"},
				Name: "subshell",
			},
			List: []build.Expr{
				&build.Ident{Name: name},
			},
		}, false, nil
	case *syntax.DeclClause:
		vals := []*build.KeyValueExpr{}

		for _, assign := range cmd.Args {
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

		return &build.CallExpr{
			X: &build.DotExpr{
				X:    &build.Ident{Name: "ctx"},
				Name: "declare",
			},
			List: []build.Expr{
				&build.StringExpr{Value: cmd.Variant.Value},
				&build.DictExpr{
					List: vals,
				},
			},
		}, false, nil
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
		var fd build.Expr = &build.StringExpr{
			Value: "1", // stdout
		}

		if redir.N != nil {
			fd = &build.StringExpr{
				Value: redir.N.Value,
			}
		}

		var hdoc build.Expr

		if redir.Hdoc != nil {
			hdoc, err = sh.translateWord(target, redir.Hdoc)
			if err != nil {
				return nil, false, err
			}
		}

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
		case syntax.AppOut:
			redirectTo, err := sh.translateWord(target, redir.Word)
			if err != nil {
				return nil, false, err
			}

			top = &build.CallExpr{
				X: &build.DotExpr{
					X:    top,
					Name: "redirect_append",
				},
				List: []build.Expr{fd, redirectTo},
			}
		// case syntax.RdrIn:
		// 	redirectTo, err := sh.translateWord(target, redir.Word)
		// 	if err != nil {
		// 		return nil, false, err
		// 	}

		// 	top = &build.CallExpr{
		// 		X: &build.DotExpr{
		// 			X:    top,
		// 			Name: "redirect_in",
		// 		},
		// 		List: []build.Expr{fd, redirectTo},
		// 	}
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
				List: []build.Expr{redirectTo, fd},
			}
		case syntax.Hdoc:
			top = &build.CallExpr{
				X: &build.DotExpr{
					X:    top,
					Name: "pipe_stdin",
				},
				List: []build.Expr{hdoc},
			}
		case syntax.DashHdoc:
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

func (sh *ShellScriptToStarlark) translateFile(target block, f *syntax.File) error {
	for _, stmt := range f.Stmts {
		top, isExpr, err := sh.translateStmt(target, stmt)
		if err != nil {
			return err
		}

		if top != nil {
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
	}

	return nil
}

func (sh *ShellScriptToStarlark) emit() []byte {
	return build.Format(sh.file)
}

func (sh *ShellScriptToStarlark) TranslateFile(r io.Reader, filename string) ([]byte, error) {
	parser := syntax.NewParser()

	f, err := parser.Parse(r, filename)
	if err != nil {
		return nil, err
	}

	target := &functionBlock{DefStmt: sh.mainFunc}

	if err := sh.translateFile(target, f); err != nil {
		return nil, err
	}

	sh.file.Stmt = append(sh.file.Stmt, sh.mainFunc)

	return sh.emit(), nil
}

func NewTranspiler(includeExternalScripts bool, debianMode bool) *ShellScriptToStarlark {
	return &ShellScriptToStarlark{
		includeExternalScripts: includeExternalScripts,
		debianMode:             debianMode,
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
}

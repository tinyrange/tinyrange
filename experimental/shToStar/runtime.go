package main

import (
	"bytes"
	"fmt"
	"io"
	"os"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
	"go.starlark.net/syntax"
)

type commandTarget func(ctx *shellContext, self *command) error

type command struct {
	target commandTarget
}

// Attr implements starlark.HasAttrs.
func (c *command) Attr(name string) (starlark.Value, error) {
	if name == "redirect" {
		return starlark.NewBuiltin("Command.redirect", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				fd         int
				targetFile string
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"fd", &fd,
				"targetFile", &targetFile,
			); err != nil {
				return starlark.None, err
			}

			return c, nil
		}), nil
	} else if name == "redirect_all" {
		return starlark.NewBuiltin("Command.redirect_all", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				targetFile string
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"targetFile", &targetFile,
			); err != nil {
				return starlark.None, err
			}

			return c, nil
		}), nil
	} else if name == "duplicate_out" {
		return starlark.NewBuiltin("Command.duplicate_out", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				other string
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"other", &other,
			); err != nil {
				return starlark.None, err
			}

			return c, nil
		}), nil
	} else if name == "pipe_stdin" {
		return starlark.NewBuiltin("Command.pipe_stdin", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				contents string
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"contents", &contents,
			); err != nil {
				return starlark.None, err
			}

			return c, nil
		}), nil
	} else if name == "cmp_or" {
		return starlark.NewBuiltin("Command.cmp_or", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				other *command
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"other", &other,
			); err != nil {
				return starlark.None, err
			}

			return c, nil
		}), nil
	} else if name == "cmp_and" {
		return starlark.NewBuiltin("Command.cmp_and", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				other *command
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"other", &other,
			); err != nil {
				return starlark.None, err
			}

			return c, nil
		}), nil
	} else if name == "run" {
		return starlark.NewBuiltin("Command.run", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				ctx *shellContext
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"ctx", &ctx,
			); err != nil {
				return starlark.None, err
			}

			return starlark.None, nil
		}), nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (c *command) AttrNames() []string {
	return []string{"redirect", "cmp_or"}
}

func (*command) String() string        { return "Command" }
func (*command) Type() string          { return "Command" }
func (*command) Hash() (uint32, error) { return 0, fmt.Errorf("Command is not hashable") }
func (*command) Truth() starlark.Bool  { return starlark.True }
func (*command) Freeze()               {}

var (
	_ starlark.Value    = &command{}
	_ starlark.HasAttrs = &command{}
)

func makeBuiltin(name string, target commandTarget) *starlark.Builtin {
	return starlark.NewBuiltin(name, func(
		thread *starlark.Thread,
		fn *starlark.Builtin,
		args starlark.Tuple,
		kwargs []starlark.Tuple,
	) (starlark.Value, error) {
		return &command{target: target}, nil
	})
}

type variable struct {
	ctx  *shellContext
	name string
}

func (*variable) String() string        { return "Variable" }
func (*variable) Type() string          { return "Variable" }
func (*variable) Hash() (uint32, error) { return 0, fmt.Errorf("Variable is not hashable") }
func (*variable) Truth() starlark.Bool  { return starlark.True }
func (*variable) Freeze()               {}

var (
	_ starlark.Value = &variable{}
)

type shellContext struct {
	rt     *ShellScriptToStarlarkRuntime
	stdout io.Writer
	stderr io.Writer
	stdin  io.Reader
}

func (s *shellContext) getVariable(name string) string {
	return ""
}

func (s *shellContext) subshell(stderr io.Writer, stdin io.Reader, f starlark.Callable) (string, error) {
	stdout := new(bytes.Buffer)

	ctx := s.rt.newContext(stdout, stderr, stdin)

	if err := s.rt.call(ctx, f); err != nil {
		return "", err
	}

	return stdout.String(), nil
}

// Attr implements starlark.HasAttrs.
func (ctx *shellContext) Attr(name string) (starlark.Value, error) {
	if name == "subshell" {
		return starlark.NewBuiltin("Context.subshell", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				target starlark.Callable
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"target", &target,
			); err != nil {
				return starlark.None, err
			}

			stdout, err := ctx.subshell(ctx.stderr, ctx.stdin, target)
			if err != nil {
				return starlark.None, err
			}

			return starlark.String(stdout), nil
		}), nil
	} else if name == "check_subshell" {
		return starlark.NewBuiltin("Context.check_subshell", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				target starlark.Callable
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"target", &target,
			); err != nil {
				return starlark.None, err
			}

			_, err := ctx.subshell(ctx.stderr, ctx.stdin, target)
			if err != nil {
				return starlark.None, err
			}

			return starlark.True, nil
		}), nil
	} else if name == "compare" {
		return starlark.NewBuiltin("Context.compare", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				lhs starlark.Value
				rhs starlark.Value
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"lhs", &lhs,
				"rhs", &rhs,
			); err != nil {
				return starlark.None, err
			}

			return starlark.False, nil
		}), nil
	} else if name == "set" {
		return starlark.NewBuiltin("Context.set", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				values *starlark.Dict
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"values", &values,
			); err != nil {
				return starlark.None, err
			}

			return starlark.False, nil
		}), nil
	} else if name == "for_range" {
		return starlark.NewBuiltin("Context.for_range", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			target := args[0].(starlark.Callable)
			name := args[1].(starlark.String)

			for _, arg := range args[2:] {
				_ = arg
			}

			_ = target
			_ = name

			return starlark.None, nil
		}), nil
	} else if name == "variable" {
		return starlark.NewBuiltin("Context.variable", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				name string
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"name", &name,
			); err != nil {
				return starlark.None, err
			}

			return &variable{ctx: ctx, name: name}, nil
		}), nil
	} else if name == "join" {
		return starlark.NewBuiltin("Context.join", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			ret := ""
			for _, arg := range args {
				if variable, ok := arg.(*variable); ok {
					ret += ctx.getVariable(variable.name)
				} else {
					str, ok := starlark.AsString(arg)
					if !ok {
						return starlark.None, fmt.Errorf("could not convert %s to string", arg.Type())
					}

					ret += str
				}
			}
			return starlark.String(ret), nil
		}), nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (s *shellContext) AttrNames() []string {
	return []string{"subshell", "compare"}
}

func (*shellContext) String() string        { return "Context" }
func (*shellContext) Type() string          { return "Context" }
func (*shellContext) Hash() (uint32, error) { return 0, fmt.Errorf("Context is not hashable") }
func (*shellContext) Truth() starlark.Bool  { return starlark.True }
func (*shellContext) Freeze()               {}

var (
	_ starlark.Value    = &shellContext{}
	_ starlark.HasAttrs = &shellContext{}
)

type ShellScriptToStarlarkRuntime struct {
	defs map[string]starlark.Value
}

func (rt *ShellScriptToStarlarkRuntime) newThread(name string) *starlark.Thread {
	return &starlark.Thread{
		Name: name,
	}
}

func (rt *ShellScriptToStarlarkRuntime) runShell(args starlark.Tuple) commandTarget {
	return func(ctx *shellContext, self *command) error {
		return fmt.Errorf("runShell not implemented")
	}
}

func (rt *ShellScriptToStarlarkRuntime) getGlobals() starlark.StringDict {
	globals := make(starlark.StringDict)

	globals["call"] = starlark.NewBuiltin("call", func(
		thread *starlark.Thread,
		fn *starlark.Builtin,
		args starlark.Tuple,
		kwargs []starlark.Tuple,
	) (starlark.Value, error) {
		return &command{target: rt.runShell(args)}, nil
	})

	globals["builtin"] = &starlarkstruct.Module{
		Name: "builtin",
		Members: starlark.StringDict{
			"true": makeBuiltin("builtin.true", func(ctx *shellContext, self *command) error {
				return fmt.Errorf("builtin.true not implemented")
			}),
			"compare": makeBuiltin("builtin.compare", func(ctx *shellContext, self *command) error {
				return fmt.Errorf("builtin.compare not implemented")
			}),
			"exit": makeBuiltin("builtin.exit", func(ctx *shellContext, self *command) error {
				return fmt.Errorf("builtin.exit not implemented")
			}),
			"exec": makeBuiltin("builtin.exec", func(ctx *shellContext, self *command) error {
				return fmt.Errorf("builtin.exec not implemented")
			}),
			"cat": makeBuiltin("builtin.cat", func(ctx *shellContext, self *command) error {
				return fmt.Errorf("builtin.cat not implemented")
			}),
			"umask": makeBuiltin("builtin.umask", func(ctx *shellContext, self *command) error {
				return fmt.Errorf("builtin.umask not implemented")
			}),
			"noop": makeBuiltin("builtin.noop", func(ctx *shellContext, self *command) error {
				return nil
			}),
		},
	}

	return globals
}

func (rt *ShellScriptToStarlarkRuntime) newContext(stdout io.Writer, stderr io.Writer, stdin io.Reader) *shellContext {
	return &shellContext{
		rt: rt,
	}
}

func (rt *ShellScriptToStarlarkRuntime) call(ctx *shellContext, val starlark.Value) error {
	_, err := starlark.Call(rt.newThread("__main__"), val, starlark.Tuple{ctx}, []starlark.Tuple{})
	if err != nil {
		return err
	}

	return nil
}

func (rt *ShellScriptToStarlarkRuntime) Run(filename string, contents []byte) error {
	thread := rt.newThread("__main__")

	defs, err := starlark.ExecFileOptions(&syntax.FileOptions{}, thread, filename, contents, rt.getGlobals())
	if err != nil {
		return err
	}

	for k, v := range defs {
		rt.defs[k] = v
	}

	main, ok := rt.defs["main"]
	if !ok {
		return fmt.Errorf("could not find main function")
	}

	ctx := rt.newContext(os.Stdout, os.Stderr, os.Stdin)

	return rt.call(ctx, main)
}

func NewRuntime() *ShellScriptToStarlarkRuntime {
	return &ShellScriptToStarlarkRuntime{
		defs: map[string]starlark.Value{},
	}
}

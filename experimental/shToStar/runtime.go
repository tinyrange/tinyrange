package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strconv"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
	"go.starlark.net/syntax"
)

type errSuccess struct {
}

// Error implements error.
func (e *errSuccess) Error() string {
	return "success"
}

var (
	_ error = &errSuccess{}
)

type commandTarget func(self *command) error

type command struct {
	target commandTarget
	args   starlark.Tuple

	ctx       *shellContext
	modifiers []func() error
}

func (c *command) stdin() io.Reader {
	return c.ctx.stdin
}

func (c *command) stdout() io.Writer {
	return c.ctx.stdout
}

func (c *command) run(ctx *shellContext) error {
	c.ctx = ctx

	for _, mod := range c.modifiers {
		if err := mod(); err != nil {
			return err
		}
	}

	return c.target(c)
}

func (c *command) getArgs() ([]string, error) {
	ret := []string{}

	for _, arg := range c.args {
		if variable, ok := arg.(*variable); ok {
			ret = append(ret, c.ctx.getVariable(variable.name))
		} else {
			str, ok := starlark.AsString(arg)
			if !ok {
				return nil, fmt.Errorf("could not convert %s to string", arg.Type())
			}

			ret = append(ret, str)
		}
	}

	return ret, nil
}

func (c *command) openTarget(target string) (io.Writer, error) {
	if target == "/dev/null" {
		return io.Discard, nil
	} else {
		if c.ctx.rt.mutateHostState {
			return os.Create(target)
		} else {
			slog.Warn("opening target not implemented", "target", target)
			return io.Discard, nil
		}
	}
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
				fd         string
				targetFile string
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"fd", &fd,
				"targetFile", &targetFile,
			); err != nil {
				return starlark.None, err
			}

			fdInt, err := strconv.ParseInt(fd, 10, 64)
			if err != nil {
				return starlark.None, err
			}

			c.modifiers = append(c.modifiers, func() error {
				target, err := c.openTarget(targetFile)
				if err != nil {
					return err
				}

				switch fdInt {
				case 1:
					c.ctx.stdout = target
				case 2:
					c.ctx.stderr = target
				default:
					return fmt.Errorf("attempt to redirect with unknown fd: %d", fdInt)
				}

				return nil
			})

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

			c.modifiers = append(c.modifiers, func() error {
				target, err := c.openTarget(targetFile)
				if err != nil {
					return err
				}

				c.ctx.stdout = target
				c.ctx.stderr = target

				return nil
			})

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
				src string
				dst string
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"src", &src,
				"dst", &dst,
			); err != nil {
				return starlark.None, err
			}

			srcFd, err := strconv.ParseInt(src, 10, 64)
			if err != nil {
				return starlark.None, err
			}

			dstFd, err := strconv.ParseInt(dst, 10, 64)
			if err != nil {
				return starlark.None, err
			}

			c.modifiers = append(c.modifiers, func() error {
				switch srcFd {
				case 1:
					switch dstFd {
					case 2:
						c.ctx.stderr = c.ctx.stdout
						return nil
					default:
						return fmt.Errorf("attempt to duplicate_out with unknown destination fd: %s", dst)
					}
				case 2:
					switch dstFd {
					case 1:
						c.ctx.stdout = c.ctx.stderr
						return nil
					default:
						return fmt.Errorf("attempt to duplicate_out with unknown destination fd: %s", dst)
					}
				default:
					return fmt.Errorf("attempt to duplicate_out with unknown src fd: %d", srcFd)
				}
			})

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

			c.modifiers = append(c.modifiers, func() error {
				c.ctx.stdin = bytes.NewReader([]byte(contents))

				return nil
			})

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

			if err := c.run(ctx); err != nil {
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
		return &command{target: target, args: args}, nil
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
	values map[string]string

	cleanup []func()
}

func (s *shellContext) Close() error {
	for _, c := range s.cleanup {
		c()
	}

	return nil
}

func (s *shellContext) setVariable(key string, value string) {
	s.values[key] = value
}

func (s *shellContext) getVariable(name string) string {
	return s.values[name]
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

			lhsString, ok := starlark.AsString(lhs)
			if !ok {
				return starlark.None, fmt.Errorf("could not convert %s to string", lhsString)
			}

			rhsString, ok := starlark.AsString(rhs)
			if !ok {
				return starlark.None, fmt.Errorf("could not convert %s to string", rhsString)
			}

			if lhsString == rhsString {
				return starlark.True, nil
			} else {
				return starlark.False, nil
			}
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

			for _, k := range values.Keys() {
				val, _, err := values.Get(k)
				if err != nil {
					return starlark.None, err
				}

				key, ok := starlark.AsString(k)
				if !ok {
					return starlark.None, fmt.Errorf("could not convert %s to string", k.Type())
				}

				value, ok := starlark.AsString(val)
				if !ok {
					return starlark.None, fmt.Errorf("could not convert %s to string", val.Type())
				}

				ctx.setVariable(key, value)
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
	defs            map[string]starlark.Value
	mutateHostState bool
}

func (rt *ShellScriptToStarlarkRuntime) newThread(name string) *starlark.Thread {
	return &starlark.Thread{
		Name: name,
	}
}

func (rt *ShellScriptToStarlarkRuntime) runShell(self *command) error {
	args, err := self.getArgs()
	if err != nil {
		return err
	}

	if rt.mutateHostState {
		cmd := exec.Command(args[0], args[1:]...)

		cmd.Stdout = self.ctx.stdout
		cmd.Stdin = self.ctx.stdin
		cmd.Stderr = self.ctx.stderr

		return cmd.Run()
	} else {
		slog.Info("runShell", "args", args)

		return nil
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
		return &command{target: rt.runShell, args: args}, nil
	})

	globals["builtin"] = &starlarkstruct.Module{
		Name: "builtin",
		Members: starlark.StringDict{
			"true": makeBuiltin("builtin.true", func(self *command) error {
				return nil
			}),
			"compare": makeBuiltin("builtin.compare", func(self *command) error {
				args, err := self.getArgs()
				if err != nil {
					return err
				}

				slog.Info("compare", "args", args)

				return nil
			}),
			"exit": makeBuiltin("builtin.exit", func(self *command) error {
				args, err := self.getArgs()
				if err != nil {
					return err
				}

				code := args[0]

				codeInt, err := strconv.ParseInt(code, 0, 64)
				if err != nil {
					return err
				}

				if codeInt == 0 {
					return &errSuccess{}
				} else {
					return fmt.Errorf("exit %d", codeInt)
				}
			}),
			"exec": makeBuiltin("builtin.exec", func(self *command) error {
				args, err := self.getArgs()
				if err != nil {
					return err
				}

				slog.Info("exec", "args", args)

				return nil
			}),
			"cat": makeBuiltin("builtin.cat", func(self *command) error {
				stdin := self.stdin()
				stdout := self.stdout()

				if _, err := io.Copy(stdout, stdin); err != nil {
					return err
				}

				return nil
			}),
			"umask": makeBuiltin("builtin.umask", func(self *command) error {
				args, err := self.getArgs()
				if err != nil {
					return err
				}

				slog.Info("umask", "args", args)

				return nil
			}),
			"readlink": makeBuiltin("builtin.readlink", func(self *command) error {
				args, err := self.getArgs()
				if err != nil {
					return err
				}

				slog.Info("readlink", "args", args)

				return nil
			}),
			"noop": makeBuiltin("builtin.noop", func(self *command) error {
				return nil
			}),
		},
	}

	return globals
}

func (rt *ShellScriptToStarlarkRuntime) newContext(stdout io.Writer, stderr io.Writer, stdin io.Reader) *shellContext {
	return &shellContext{
		rt:     rt,
		stdout: stdout,
		stderr: stderr,
		stdin:  stdin,
		values: make(map[string]string),
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

	err = rt.call(ctx, main)
	if errors.Is(err, &errSuccess{}) {
		return nil
	} else if err != nil {
		return err
	} else {
		return nil
	}
}

func NewRuntime(mutateHostState bool) *ShellScriptToStarlarkRuntime {
	return &ShellScriptToStarlarkRuntime{
		defs:            map[string]starlark.Value{},
		mutateHostState: mutateHostState,
	}
}

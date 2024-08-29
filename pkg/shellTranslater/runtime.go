package shelltranslater

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/tinyrange/tinyrange/pkg/common"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
	"go.starlark.net/syntax"
	"golang.org/x/sys/unix"
)

type errExitCode int

// Error implements error.
func (e errExitCode) Error() string {
	return fmt.Sprintf("exit code %d", e)
}

type errVariableNotFound string

// Error implements error.
func (e errVariableNotFound) Error() string {
	return fmt.Sprintf("variable %s not found", string(e))
}

type errReturn string

// Error implements error.
func (e errReturn) Error() string {
	return "flow return"
}

type errContinue string

// Error implements error.
func (e errContinue) Error() string {
	return "flow continue"
}

var (
	_ error = errExitCode(0)
	_ error = errVariableNotFound("")
	_ error = errReturn("")
	_ error = errContinue("")
)

type commandTarget func(self *command, args []string) error

type command struct {
	target commandTarget
	args   starlark.Tuple

	ctx       *shellContext
	modifiers []func() error
	onFailure *command
	onSuccess *command
	negated   bool
}

func (c *command) stdin() io.Reader { return c.ctx.stdin }

func (c *command) stdout() io.Writer { return c.ctx.stdout }

func (c *command) stderr() io.Writer { return c.ctx.stderr }

func (c *command) run(ctx *shellContext) (int, error) {
	c.ctx = ctx

	for _, mod := range c.modifiers {
		if err := mod(); err != nil {
			return -1, err
		}
	}

	args, err := c.getArgs()
	if err != nil {
		return -1, err
	}

	if err := c.target(c, args); err != nil {
		exitCode := errExitCode(0)
		if errors.As(err, &exitCode) {
			if c.negated {
				if exitCode == 0 {
					exitCode = errExitCode(1)
				} else {
					exitCode = errExitCode(0)
				}

				if c.onSuccess != nil {
					return c.onSuccess.run(ctx)
				} else {
					return int(exitCode), nil
				}
			} else {
				if c.onFailure != nil {
					return c.onFailure.run(ctx)
				} else {
					return int(exitCode), nil
				}
			}
		} else {
			return -1, err
		}
	}

	if c.negated {
		if c.onFailure != nil {
			return c.onFailure.run(ctx)
		}

		return 1, nil
	} else {
		if c.onSuccess != nil {
			return c.onSuccess.run(ctx)
		}

		return 0, nil
	}
}

func (c *command) getArgs() ([]string, error) {
	ret := []string{}

	for _, arg := range c.args {
		val, err := c.ctx.getString(arg)
		if err != nil {
			return nil, err
		}

		ret = append(ret, val)
	}

	return ret, nil
}

func (c *command) openTarget(target string, append bool) (io.Writer, error) {
	if target == "/dev/null" {
		return io.Discard, nil
	} else {
		if c.ctx.rt.mutateHostState {
			if append {
				return os.OpenFile(target, os.O_APPEND|os.O_WRONLY, os.ModePerm)
			} else {
				return os.Create(target)
			}
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
				target, err := c.openTarget(targetFile, false)
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
	} else if name == "redirect_append" {
		return starlark.NewBuiltin("Command.redirect_append", func(
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
				target, err := c.openTarget(targetFile, true)
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
				target, err := c.openTarget(targetFile, false)
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

			c.onFailure = other

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

			c.onSuccess = other

			return c, nil
		}), nil
	} else if name == "negated" {
		return starlark.NewBuiltin("Command.negated", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			c.negated = true

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

			exitCode, err := c.run(ctx)
			if err != nil {
				return starlark.None, err
			}

			if exitCode == 0 {
				return starlark.None, nil
			}

			if ctx.terminateOnError {
				return starlark.None, errExitCode(exitCode)
			} else {
				return starlark.None, nil
			}
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
	name string
	def  *string
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
	parent *shellContext
	stdout io.Writer
	stderr io.Writer
	stdin  io.Reader
	values map[string]string

	// shell options
	terminateOnError bool

	cleanup []func()
}

func (s *shellContext) Close() error {
	for _, c := range s.cleanup {
		c()
	}

	return nil
}

func (s *shellContext) setArguments(args []string) {
	slog.Debug("setArguments", "args", args)
	s.setVariable("@", strings.Join(args, " "))
	for i, arg := range args {
		s.setVariable(strconv.Itoa(i), arg)
	}
}

func (s *shellContext) setEnvironment(env map[string]string) error {
	for k, v := range env {
		s.setVariable(k, v)

		if err := os.Setenv(k, v); err != nil {
			return err
		}
	}
	return nil
}

func (s *shellContext) setVariable(key string, value string) {
	s.values[key] = value
}

func (s *shellContext) getVariable(name string) (string, error) {
	val, ok := s.values[name]
	if ok {
		return val, nil
	}

	if s.parent != nil {
		val, err := s.parent.getVariable(name)
		if err == nil {
			return val, nil
		}
	}

	return "", errVariableNotFound(name)
}

func (s *shellContext) subshell(stderr io.Writer, stdin io.Reader, f starlark.Callable, check bool) (string, error) {
	stdout := new(bytes.Buffer)

	ctx := s.rt.newContext(s, stdout, stderr, stdin)

	if check {
		ctx.terminateOnError = true
	}

	if err := s.rt.call(ctx, f); err != nil {
		return "", err
	}

	return stdout.String(), nil
}

func (ctx *shellContext) getString(val starlark.Value) (string, error) {
	if variable, ok := val.(*variable); ok {
		val, err := ctx.getVariable(variable.name)
		if err != nil {
			if variable.def != nil {
				return *variable.def, nil
			} else {
				return "", err
			}
		}

		return val, nil
	} else {
		str, ok := starlark.AsString(val)
		if !ok {
			return "", fmt.Errorf("could not convert %s to string", val.Type())
		}

		return str, nil
	}
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

			stdout, err := ctx.subshell(ctx.stderr, ctx.stdin, target, false)
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

			_, err := ctx.subshell(ctx.stderr, ctx.stdin, target, true)
			exitCode := errExitCode(0)
			if errors.As(err, &exitCode) {
				if exitCode == 0 {
					return starlark.True, nil
				} else {
					return starlark.False, nil
				}
			} else if err != nil {
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
			lhs := args[0]
			opts := args[1:]

			lhsString, err := ctx.getString(lhs)
			if err != nil {
				return starlark.None, err
			}

			for _, opt := range opts {
				val, err := ctx.getString(opt)
				if err != nil {
					return starlark.None, err
				}

				if lhsString == val {
					return starlark.True, nil
				}
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

			for _, k := range values.Keys() {
				val, _, err := values.Get(k)
				if err != nil {
					return starlark.None, err
				}

				key, ok := starlark.AsString(k)
				if !ok {
					return starlark.None, fmt.Errorf("could not convert %s to string", k.Type())
				}

				value, err := ctx.getString(val)
				if err != nil {
					return starlark.None, err
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

			var argList []string

			for _, arg := range args[2:] {
				val, err := ctx.getString(arg)
				if err != nil {
					return nil, err
				}
				argList = append(argList, val)
			}

			for _, arg := range argList {
				ctx := ctx.rt.newContext(ctx, ctx.stdout, ctx.stderr, ctx.stdin)

				ctx.setVariable(string(name), arg)

				err := ctx.rt.call(ctx, target)
				if errors.Is(err, errContinue("")) {
					continue
				} else if err != nil {
					return starlark.None, err
				}
			}

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
				def  string
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"name", &name,
				"default?", &def,
			); err != nil {
				return starlark.None, err
			}

			return &variable{name: name, def: &def}, nil
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
				val, err := ctx.getString(arg)
				if err != nil {
					return starlark.None, err
				}

				ret += val
			}

			return starlark.String(ret), nil
		}), nil
	} else if name == "declare" {
		return starlark.NewBuiltin("Context.declare", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				kind   string
				values *starlark.Dict
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"kind", &kind,
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

				if kind == "local" {
					ctx.setVariable(key, value)
				} else {
					return starlark.None, fmt.Errorf("unknown declare kind: %s", kind)
				}
			}

			return starlark.False, nil
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

func (rt *ShellScriptToStarlarkRuntime) runShell(self *command, args []string) error {
	slog.Debug("runShell", "args", args)

	// If it's a local function that's already declared just run that.
	if def, ok := rt.defs[args[0]]; ok {
		self.ctx.setArguments(args)

		return rt.call(self.ctx, def)
	}

	if rt.mutateHostState {
		start := time.Now()

		cmd := exec.Command(args[0], args[1:]...)

		cmd.Stdout = self.ctx.stdout
		cmd.Stdin = self.ctx.stdin
		cmd.Stderr = self.ctx.stderr

		if err := cmd.Run(); err != nil {
			slog.Debug("runShell failed", "args", args, "err", err, "took", time.Since(start))

			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				return fmt.Errorf("failed to run %s: %w", args, errExitCode(exitErr.ExitCode()))
			} else {
				return fmt.Errorf("failed to run %s: %w", args, err)
			}
		}

		slog.Debug("runShell", "args", args, "took", time.Since(start))

		return nil
	} else {
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
			"true": makeBuiltin("builtin.true", func(self *command, args []string) error {
				return nil
			}),
			"test": makeBuiltin("builtin.test", func(self *command, args []string) error {
				if args[0] == "!" {
					if args[1] == "-e" {
						if self.ctx.rt.mutateHostState {
							if ok, _ := common.Exists(args[2]); ok {
								return errExitCode(1)
							} else {
								return nil
							}
						} else {
							return fmt.Errorf("test -e not supported without mutating host state")
						}
					} else if args[1] == "-f" {
						if self.ctx.rt.mutateHostState {
							info, err := os.Stat(args[2])
							if err != nil {
								fmt.Fprintf(self.stderr(), "failed to stat: %s\n", err)
								return errExitCode(1)
							}

							if info.Mode().IsRegular() {
								return errExitCode(1)
							} else {
								return nil
							}
						} else {
							return fmt.Errorf("test -f not supported without mutating host state")
						}
					} else if args[1] == "-L" {
						if self.ctx.rt.mutateHostState {
							info, err := os.Stat(args[2])
							if err != nil {
								fmt.Fprintf(self.stderr(), "failed to stat: %s\n", err)
								return errExitCode(1)
							}

							if info.Mode()&fs.ModeSymlink != 0 {
								return errExitCode(1)
							} else {
								return nil
							}
						} else {
							return fmt.Errorf("test -x not supported without mutating host state")
						}
					} else {
						return fmt.Errorf("unimplemented test command: %+v", args)
					}
				} else if args[0] == "-n" {
					if args[1] != "" {
						return nil
					} else {
						return errExitCode(1)
					}
				} else if args[0] == "-z" {
					if args[1] == "" {
						return nil
					} else {
						return errExitCode(1)
					}
				} else if args[0] == "-e" {
					if self.ctx.rt.mutateHostState {
						if ok, _ := common.Exists(args[1]); ok {
							return nil
						} else {
							return errExitCode(1)
						}
					} else {
						return fmt.Errorf("test -e not supported without mutating host state")
					}
				} else if args[0] == "-f" {
					if self.ctx.rt.mutateHostState {
						info, err := os.Stat(args[1])
						if err != nil {
							fmt.Fprintf(self.stderr(), "failed to stat: %s\n", err)
							return errExitCode(1)
						}

						if info.Mode().IsRegular() {
							return nil
						} else {
							return errExitCode(1)
						}
					} else {
						return fmt.Errorf("test -f not supported without mutating host state")
					}
				} else if args[0] == "-d" {
					if self.ctx.rt.mutateHostState {
						info, err := os.Stat(args[1])
						if err != nil {
							fmt.Fprintf(self.stderr(), "failed to stat: %s\n", err)
							return errExitCode(1)
						}

						if info.Mode().IsDir() {
							return nil
						} else {
							return errExitCode(1)
						}
					} else {
						return fmt.Errorf("test -d not supported without mutating host state")
					}
				} else if args[0] == "-x" {
					if self.ctx.rt.mutateHostState {
						info, err := os.Stat(args[1])
						if err != nil {
							fmt.Fprintf(self.stderr(), "failed to stat: %s\n", err)
							return errExitCode(1)
						}

						if info.Mode().Perm()&0111 != 0 {
							return nil
						} else {
							return errExitCode(1)
						}
					} else {
						return fmt.Errorf("test -x not supported without mutating host state")
					}
				} else if args[0] == "-L" {
					if self.ctx.rt.mutateHostState {
						info, err := os.Stat(args[1])
						if err != nil {
							fmt.Fprintf(self.stderr(), "failed to stat: %s\n", err)
							return errExitCode(1)
						}

						if info.Mode()&fs.ModeSymlink != 0 {
							return nil
						} else {
							return errExitCode(1)
						}
					} else {
						return fmt.Errorf("test -x not supported without mutating host state")
					}
				} else if args[1] == "=" {
					if args[0] == args[2] {
						return nil
					} else {
						return errExitCode(1)
					}
				} else if args[1] == "!=" {
					if args[0] != args[2] {
						return nil
					} else {
						return errExitCode(1)
					}
				} else {
					return fmt.Errorf("unimplemented test command: %+v", args)
				}
			}),
			"exit": makeBuiltin("builtin.exit", func(self *command, args []string) error {
				code := args[0]

				codeInt, err := strconv.ParseInt(code, 0, 64)
				if err != nil {
					return err
				}

				return errExitCode(codeInt)
			}),
			"exec": makeBuiltin("builtin.exec", func(self *command, args []string) error {
				if self.ctx.rt.mutateHostState {
					slog.Debug("exec", "args", args)

					filename, err := exec.LookPath(args[0])
					if err != nil {
						return err
					}
					return unix.Exec(filename, args, os.Environ())
				} else {
					return fmt.Errorf("exec not implemented for %+v", args)
				}
			}),
			"cat": makeBuiltin("builtin.cat", func(self *command, args []string) error {
				stdin := self.stdin()
				stdout := self.stdout()

				if _, err := io.Copy(stdout, stdin); err != nil {
					return err
				}

				return nil
			}),
			"umask": makeBuiltin("builtin.umask", func(self *command, args []string) error {
				if self.ctx.rt.mutateHostState {
					val, err := strconv.ParseInt(args[0], 8, 64)
					if err != nil {
						return err
					}

					unix.Umask(int(val))

					return nil
				} else {
					return fmt.Errorf("builtin.umask not implemented: %+v", args)
				}
			}),
			"readlink": makeBuiltin("builtin.readlink", func(self *command, args []string) error {
				val, err := os.Readlink(args[0])
				if err != nil {
					fmt.Fprintf(self.stderr(), "failed to readlink: %s\n", err)
					return errExitCode(1)
				}

				fmt.Fprintf(self.stdout(), "%s", val)

				return nil
			}),
			"noop": makeBuiltin("builtin.noop", func(self *command, args []string) error {
				return nil
			}),
			"set": makeBuiltin("builtin.set", func(self *command, args []string) error {
				if args[0] == "-e" {
					self.ctx.terminateOnError = true

					return nil
				} else if args[0] == "+e" {
					self.ctx.terminateOnError = false

					return nil
				} else {
					return fmt.Errorf("unimplemented set command: %+v", args)
				}
			}),
			"echo": makeBuiltin("builtin.echo", func(self *command, args []string) error {
				fmt.Fprintf(self.stdout(), "%s\n", strings.Join(args, " "))

				return nil
			}),
			"which": makeBuiltin("builtin.which", func(self *command, args []string) error {
				p, err := exec.LookPath(args[0])
				if err != nil {
					fmt.Fprintf(self.stderr(), "%s\n", err)
					return errExitCode(1)
				}

				fmt.Fprintf(self.stdout(), "%s\n", p)

				return nil
			}),
			"eval": makeBuiltin("builtin.eval", func(self *command, args []string) error {
				if args[0] == "" {
					return nil
				}

				return self.ctx.rt.eval(self.ctx, args[0])
			}),
			"cd": makeBuiltin("builtin.cd", func(self *command, args []string) error {
				return os.Chdir(args[0])
			}),
			"command": makeBuiltin("builtin.command", func(self *command, args []string) error {
				if args[0] == "-v" {
					p, err := exec.LookPath(args[1])
					if err != nil {
						fmt.Fprintf(self.stderr(), "%s\n", err)
						return errExitCode(1)
					}

					fmt.Fprintf(self.stdout(), "%s\n", p)

					return nil
				} else {
					return fmt.Errorf("builtin.command not implemented: %+v", args)
				}
			}),
			"flow_return": makeBuiltin("builtin.flow_return", func(self *command, args []string) error {
				return errReturn("")
			}),
			"flow_continue": makeBuiltin("builtin.flow_continue", func(self *command, args []string) error {
				return errContinue("")
			}),
		},
	}

	globals["debian"] = &starlarkstruct.Module{
		Name: "debian",
		Members: starlark.StringDict{
			"dpkg_maintscript_helper": makeBuiltin("debian.dpkg_maintscript_helper", func(self *command, args []string) error {
				slog.Debug("debian.dpkg_maintscript_helper", "args", args)

				return nil
			}),
			"update_rc_d": makeBuiltin("debian.update_rc_d", func(self *command, args []string) error {
				slog.Debug("debian.update_rc_d", "args", args)

				return nil
			}),
			"invoke_rc_d": makeBuiltin("debian.invoke_rc_d", func(self *command, args []string) error {
				slog.Debug("debian.invoke_rc_d", "args", args)

				return nil
			}),
			"py3compile": makeBuiltin("debian.py3compile", func(self *command, args []string) error {
				slog.Debug("debian.py3compile", "args", args)

				return nil
			}),
			"deb_systemd_helper": makeBuiltin("debian.deb_systemd_helper", func(self *command, args []string) error {
				slog.Debug("debian.deb_systemd_helper", "args", args)

				return nil
			}),
		},
	}

	return globals
}

func (rt *ShellScriptToStarlarkRuntime) newContext(parent *shellContext, stdout io.Writer, stderr io.Writer, stdin io.Reader) *shellContext {
	return &shellContext{
		rt:     rt,
		parent: parent,
		stdout: stdout,
		stderr: stderr,
		stdin:  stdin,
		values: make(map[string]string),
	}
}

func (rt *ShellScriptToStarlarkRuntime) call(ctx *shellContext, val starlark.Value) error {
	_, err := starlark.Call(rt.newThread("__main__"), val, starlark.Tuple{ctx}, []starlark.Tuple{})
	if errors.Is(err, errReturn("")) {
		return nil
	} else if err != nil {
		return err
	}

	return nil
}

func (rt *ShellScriptToStarlarkRuntime) eval(ctx *shellContext, script string) error {
	sh := NewTranspiler(true, true)

	out, err := sh.TranslateFile(bytes.NewReader([]byte(script)), "eval")
	if err != nil {
		return err
	}

	slog.Debug("translated for", "output", string(out), "script", script)

	thread := rt.newThread("__main__")

	defs, err := starlark.ExecFileOptions(&syntax.FileOptions{}, thread, "eval", out, rt.getGlobals())
	if err != nil {
		return err
	}

	main, ok := defs["main"]
	if !ok {
		return fmt.Errorf("could not find main function")
	}

	return rt.call(ctx, main)
}

func (rt *ShellScriptToStarlarkRuntime) Run(filename string, contents []byte, args []string, environment map[string]string) error {
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

	ctx := rt.newContext(nil, os.Stdout, os.Stderr, os.Stdin)

	if err := ctx.setEnvironment(environment); err != nil {
		return err
	}

	ctx.setArguments(args)

	err = rt.call(ctx, main)
	exitCode := errExitCode(0)
	if errors.As(err, &exitCode) {
		if exitCode == 0 {
			return nil
		} else {
			return exitCode
		}
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

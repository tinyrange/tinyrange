package shell

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"

	"github.com/tinyrange/tinyrange/pkg/emulator/shared"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"mvdan.cc/sh/v3/syntax"
)

type shellProgram struct {
	filesystem.File

	proc     shared.Process
	locals   shared.Environment
	builtIns map[string]func(args []string) error
}

func (sh *shellProgram) getEnv(name string) string {
	if sh.locals.Has(name) {
		return sh.locals.Get(name)
	}

	return sh.proc.Getenv(name)
}

func (sh *shellProgram) runProgram(args []string, env shared.Environment) error {
	if builtin, ok := sh.builtIns[args[0]]; ok {
		return builtin(args)
	}

	// slog.Info("runProgram", "args", args)

	proc, err := sh.proc.Fork()
	if err != nil {
		return err
	}

	for k, v := range env {
		proc.Setenv(k, v)
	}

	return proc.Exec(args)
}

func (sh *shellProgram) subShell(stdin io.Reader, redirectStdout bool, inner func(sh *shellProgram) error) (string, error) {
	proc, err := sh.proc.Fork()
	if err != nil {
		return "", err
	}

	if stdin != nil {
		proc.SetStdin(stdin)
	}

	out := new(bytes.Buffer)

	if redirectStdout {
		proc.SetStdout(out)
	}

	newSh := sh.Create().(*shellProgram)

	newSh.init()

	newSh.proc = proc

	if err := inner(newSh); err != nil {
		return "", err
	}

	return out.String(), nil
}

func (sh *shellProgram) evalAssign(assign *syntax.Assign) (string, string, error) {
	k := assign.Name.Value

	if assign.Value != nil {
		val, err := sh.evalWord(assign.Value)
		if err != nil {
			return "", "", err
		}

		return k, val, nil
	}

	return "", "", fmt.Errorf("evalAssign without value not implemented")
}

func (sh *shellProgram) evalWord(word *syntax.Word) (string, error) {
	var ret []string

	for _, part := range word.Parts {
		switch part := part.(type) {
		case *syntax.Lit:
			ret = append(ret, part.Value)
		case *syntax.SglQuoted:
			if part.Dollar {
				return "", fmt.Errorf("single quotes with dollar not implemented")
			} else {
				ret = append(ret, part.Value)
			}
		case *syntax.DblQuoted:
			val, err := sh.evalWord(&syntax.Word{Parts: part.Parts})
			if err != nil {
				return "", err
			}

			ret = append(ret, val)
		case *syntax.CmdSubst:
			res, err := sh.subShell(nil, true, func(sh *shellProgram) error {
				for _, stmt := range part.Stmts {
					if err := sh.evalStmt(stmt); err != nil {
						return err
					}
				}

				return nil
			})
			if err != nil {
				return "", err
			}

			ret = append(ret, res)
		case *syntax.ParamExp:
			if part.Dollar.IsValid() {
				ret = append(ret, sh.getEnv(part.Param.Value))
			} else {
				return "", fmt.Errorf("syntax.ParamExp %+v not implemented", part)
			}
		default:
			return "", fmt.Errorf("word part %T not implemented", part)
		}
	}

	return strings.Join(ret, ""), nil
}

func (sh *shellProgram) evalStmt(stmt *syntax.Stmt) error {
	switch cmd := stmt.Cmd.(type) {
	case *syntax.CallExpr:
		env := make(shared.Environment)

		for _, assign := range cmd.Assigns {
			k, v, err := sh.evalAssign(assign)
			if err != nil {
				return err
			}

			env.Set(k, v)
		}

		if len(cmd.Args) > 0 {
			var args []string

			for _, arg := range cmd.Args {
				val, err := sh.evalWord(arg)
				if err != nil {
					return err
				}
				args = append(args, val)
			}

			return sh.runProgram(args, env)
		} else {
			// load all the assignments into the local shell environment.
			for k, v := range env {
				sh.locals.Set(k, v)
			}

			return nil
		}
	case *syntax.BinaryCmd:
		switch cmd.Op {
		case syntax.Pipe:
			out, err := sh.subShell(nil, true, func(sh *shellProgram) error {
				return sh.evalStmt(cmd.X)
			})
			if err != nil {
				return err
			}

			_, err = sh.subShell(bytes.NewReader([]byte(out)), false, func(sh *shellProgram) error {
				return sh.evalStmt(cmd.Y)
			})
			if err != nil {
				return err
			}

			return nil
		default:
			return fmt.Errorf("BinaryCmd op %s not implemented", cmd.Op.String())
		}
	case *syntax.DeclClause:
		switch cmd.Variant.Value {
		case "export":
			for _, assign := range cmd.Args {
				k, v, err := sh.evalAssign(assign)
				if err != nil {
					return err
				}

				sh.proc.Setenv(k, v)
			}

			return nil
		default:
			return fmt.Errorf("DeclClause: %s not implemented", cmd.Variant.Value)
		}
	case *syntax.IfClause:
		// ignore
		return nil
	default:
		return fmt.Errorf("command %T not implemented", cmd)
	}
}

func (sh *shellProgram) evalFile(f *syntax.File) error {
	for _, stmt := range f.Stmts {
		if err := sh.evalStmt(stmt); err != nil {
			return fmt.Errorf("shell: failed to evaluate statement: %s", err)
		}
	}

	return nil
}

func (sh *shellProgram) init() {
	sh.builtIns["set"] = func(args []string) error {
		slog.Info("set", "args", args)

		return nil
	}

	sh.builtIns["head"] = func(args []string) error {
		if len(args) == 3 && args[1] == "-n" {
			count, err := strconv.ParseInt(args[2], 10, 64)
			if err != nil {
				return err
			}

			scanner := bufio.NewScanner(sh.proc)

			var i int64
			for i = 0; i < count; i++ {
				ok := scanner.Scan()
				if !ok {
					break
				}

				if _, err := sh.proc.Write(scanner.Bytes()); err != nil {
					return err
				}
			}

			return nil
		} else {
			return fmt.Errorf("head not implemented: %+v", args)
		}
	}

	sh.builtIns["cut"] = func(args []string) error {
		if len(args) == 5 && args[1] == "-d" && args[3] == "-f" {
			data, err := io.ReadAll(sh.proc)
			if err != nil {
				return err
			}

			slog.Info("", "data", data)

			tokens := strings.Split(string(data), args[2])

			index, err := strconv.ParseInt(args[4], 10, 64)
			if err != nil {
				return err
			}

			if index > int64(len(tokens)) {
				return fmt.Errorf("attempt to get token out of range: %d > %d", index, len(tokens))
			}

			if _, err := fmt.Fprintf(sh.proc, "%s", tokens[index-1]); err != nil {
				return err
			}

			return nil
		} else {
			return fmt.Errorf("cut not implemented: %+v", args)
		}
	}

	sh.builtIns["tr"] = func(args []string) error {
		if len(args) == 3 {
			data, err := io.ReadAll(sh.proc)
			if err != nil {
				return err
			}

			out := strings.ReplaceAll(string(data), args[1], args[2])

			if _, err := fmt.Fprintf(sh.proc, "%s", out); err != nil {
				return err
			}

			return nil
		} else {
			return fmt.Errorf("tr not implemented: %+v", args)
		}
	}

	sh.builtIns["echo"] = func(args []string) error {
		if len(args) > 1 {
			if _, err := fmt.Fprintf(sh.proc, "%s\n", strings.Join(args[1:], " ")); err != nil {
				return nil
			}

			return nil
		} else {
			return fmt.Errorf("echo not implemented: %+v", args)
		}
	}

	sh.builtIns["source"] = func(args []string) error {
		if len(args) == 2 {
			filename := args[1]

			return sh.sourceFile(filename)
		} else {
			return fmt.Errorf("source not implemented: %+v", args)
		}
	}

	sh.builtIns["cd"] = func(args []string) error {
		if len(args) == 2 {
			newPath := args[1]

			return sh.proc.Chdir(newPath)
		} else {
			return fmt.Errorf("cd not implemented: %+v", args)
		}
	}

	sh.builtIns["yes"] = func(args []string) error {
		return nil // stub
	}

	sh.builtIns["cat"] = func(args []string) error {
		if len(args) == 2 {
			f, err := sh.proc.Open(args[1])
			if err != nil {
				return err
			}
			defer f.Close()

			if _, err := io.Copy(sh.proc, f); err != nil {
				return err
			}

			return nil
		} else {
			return fmt.Errorf("cd not implemented: %+v", args)
		}
	}
}

func (sh *shellProgram) sourceFile(filename string) error {
	fh, err := sh.proc.Open(filename)
	if err != nil {
		return err
	}
	defer fh.Close()

	parser := syntax.NewParser(syntax.Variant(syntax.LangBash))

	f, err := parser.Parse(fh, filename)
	if err != nil {
		return err
	}

	// if filename == "build.sh" {
	// 	marshalled, err := json.Marshal(f)
	// 	if err != nil {
	// 		return err
	// 	}

	// 	if err := os.WriteFile("local/parsed.json", marshalled, os.ModePerm); err != nil {
	// 		return err
	// 	}
	// }

	return sh.evalFile(f)
}

// Run implements shared.Program.
func (sh *shellProgram) Run(proc shared.Process, argv []string) error {
	if len(argv) < 2 {
		return fmt.Errorf("usage: shell <filename>")
	}

	// slog.Info("shell main", "argv", argv)

	sh.init()

	sh.proc = proc

	if argv[1] == "-c" {
		parser := syntax.NewParser(syntax.Variant(syntax.LangBash))

		f, err := parser.Parse(bytes.NewReader([]byte(argv[2])), "<stdin>")
		if err != nil {
			return err
		}

		return sh.evalFile(f)
	} else {
		return sh.sourceFile(argv[1])
	}
}

// Create implements shared.Program.
func (sh *shellProgram) Create() shared.Program {
	return &shellProgram{
		File:     filesystem.NewMemoryFile(filesystem.TypeRegular),
		builtIns: make(map[string]func(args []string) error),
		locals:   make(shared.Environment),
	}
}

func NewShellProgram() shared.Program {
	return (&shellProgram{}).Create()
}

package programs

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"

	fsCommon "github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/emulator/common"
	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/interp"
	"mvdan.cc/sh/v3/syntax"
)

type readOnlyFile struct{ io.ReadCloser }

// Write implements io.ReadWriteCloser.
func (r *readOnlyFile) Write(p []byte) (n int, err error) {
	return 0, fs.ErrInvalid
}

var (
	_ io.ReadWriteCloser = &readOnlyFile{}
)

type devNull struct{}

// Close implements io.ReadWriteCloser.
func (d *devNull) Close() error {
	return nil
}

// Read implements io.ReadWriteCloser.
func (d *devNull) Read(p []byte) (n int, err error) {
	return 0, io.EOF
}

// Write implements io.ReadWriteCloser.
func (d *devNull) Write(p []byte) (n int, err error) {
	return len(p), nil
}

const (
	// Exactly one of O_RDONLY, O_WRONLY, or O_RDWR must be specified.
	O_RDONLY int = 0 // open the file read-only.
	O_WRONLY int = 1 // open the file write-only.
	O_RDWR   int = 2 // open the file read-write.
	// The remaining values may be or'ed in to control behavior.
	O_APPEND int = 0x400    // append data to the file when writing.
	O_CREATE int = 0x40     // create a new file if none exists.
	O_EXCL   int = 0x80     // used with O_CREATE, file must not exist.
	O_SYNC   int = 0x101000 // open for synchronous I/O.
	O_TRUNC  int = 0x200    // truncate regular writable file when opened.
)

type Shell struct {
	fsCommon.File

	proc common.Process
}

func (sh *Shell) runReader(fh io.Reader, args []string) error {
	interp, err := interp.New(
		interp.OpenHandler(func(ctx context.Context, path string, flag int, perm os.FileMode) (io.ReadWriteCloser, error) {
			slog.Info("interp open", "path", path, "flag", flag, "perm", perm)
			if path == "/dev/null" {
				return &devNull{}, nil
			}

			if flag == 0 {
				fh, err := sh.proc.Open(path)
				if err != nil {
					return nil, err
				}

				return &readOnlyFile{fh}, nil
			}

			return nil, fmt.Errorf("open not implemented, path: %s, flag %d, perm %s", path, flag, perm)
		}),
		interp.ReadDirHandler2(func(ctx context.Context, path string) ([]fs.DirEntry, error) {
			slog.Info("interp readdir", "path", path)
			return nil, fmt.Errorf("readdir not implemented, path: %s", path)
		}),
		interp.StatHandler(func(ctx context.Context, name string, followSymlinks bool) (fs.FileInfo, error) {
			slog.Info("interp stat", "name", name, "followSymlinks", followSymlinks)
			return nil, fmt.Errorf("stat not implemented, name: %s", name)
		}),
		interp.Env(expand.FuncEnviron(func(s string) string {
			slog.Info("interp getenv", "key", s)
			return sh.proc.Getenv(s)
		})),
		interp.Params(args...),
		interp.ExecHandlers(func(next interp.ExecHandlerFunc) interp.ExecHandlerFunc {
			return func(ctx context.Context, args []string) error {
				slog.Info("interp exec", "args", args)
				hc := interp.HandlerCtx(ctx)

				env := map[string]string{}

				for k, v := range sh.proc.Environ() {
					env[k] = v
				}

				hc.Env.Each(func(name string, vr expand.Variable) bool {
					if !vr.Exported {
						return true
					}

					env[name] = vr.String()

					return true
				})

				slog.Info("exec", "env", env)

				err := sh.proc.Spawn(hc.Dir, args, env, hc.Stdin, hc.Stdout, hc.Stderr)
				if err == fs.ErrNotExist {
					return next(ctx, args)
				} else if err != nil {
					return err
				}

				return nil
			}
		}),
		interp.StdIO(sh.proc.Stdin(), sh.proc.Stdout(), sh.proc.Stderr()),
	)
	if err != nil {
		return fmt.Errorf("failed to create shell: %s", err)
	}
	interp.Dir = sh.proc.Getwd()

	parser := syntax.NewParser()

	parser.Stmts(fh, func(s *syntax.Stmt) bool {
		err = interp.Run(context.Background(), s)
		return err == nil
	})
	if err != nil {
		return fmt.Errorf("failed to run: %w", err)
	}

	return nil
}

func (sh *Shell) runScript(filename string, args []string) error {
	fh, err := sh.proc.Open(filename)
	if err != nil {
		return err
	}
	defer fh.Close()

	return sh.runReader(fh, args)
}

func (sh *Shell) runSnippet(snippet string, args []string) error {
	return sh.runReader(bytes.NewReader([]byte(snippet)), args)
}

// Name implements common.Program.
func (sh *Shell) Name() string {
	return "sh"
}

// Run implements common.Program.
func (sh *Shell) Run(proc common.Process, argv []string) error {
	sh.proc = proc

	if len(argv) == 2 {
		return sh.runScript(argv[1], argv[1:])
	} else if len(argv) == 3 && argv[1] == "-c" {
		return sh.runSnippet(argv[2], argv[1:])
	} else {
		return fmt.Errorf("shell unimplemented: argv=%+v", argv)
	}
}

var (
	_ common.Program = &Shell{}
)

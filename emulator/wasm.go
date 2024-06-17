package emulator

import (
	"context"
	"fmt"
	"io"
	"io/fs"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"github.com/tetratelabs/wazero/sys"
	"github.com/tinyrange/pkg2/v2/emulator/common"
	"github.com/tinyrange/pkg2/v2/filesystem"
)

type deferredOpen struct {
	proc     common.Process
	filename string
	fh       io.ReadCloser
}

// Close implements fs.File.
func (d *deferredOpen) Close() error {
	if d.fh != nil {
		return d.fh.Close()
	} else {
		return nil
	}
}

// Read implements fs.File.
func (d *deferredOpen) Read(b []byte) (int, error) {
	if d.fh != nil {
		fh, err := d.proc.Open(d.filename)
		if err != nil {
			return 0, err
		}
		d.fh = fh
	}

	return d.fh.Read(b)
}

// Stat implements fs.File.
func (d *deferredOpen) Stat() (fs.FileInfo, error) {
	return d.proc.Stat(d.filename)
}

var (
	_ fs.File = &deferredOpen{}
)

type processFs struct {
	common.Process
}

// Open implements fs.FS.
// Subtle: this method shadows the method (Process).Open of processFs.Process.
func (p *processFs) Open(name string) (fs.File, error) {
	// Make sure the file exists by calling stat on it.
	info, err := p.Stat(name)
	if err != nil {
		return nil, err
	}

	_ = info

	return &deferredOpen{proc: p.Process, filename: name}, nil
}

var (
	_ fs.FS = &processFs{}
)

type wasmProgram struct {
	filesystem.File
}

// Name implements common.Program.
func (w *wasmProgram) Name() string {
	return "<wasm>"
}

// Run implements common.Program.
func (w *wasmProgram) Run(proc common.Process, argv []string) error {
	ctx := context.Background()

	f, err := w.File.Open()
	if err != nil {
		return err
	}
	defer f.Close()

	contents, err := io.ReadAll(f)
	if err != nil {
		return err
	}

	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	wasi_snapshot_preview1.MustInstantiate(ctx, rt)

	config := wazero.NewModuleConfig().
		WithArgs(argv...).
		WithFS(&processFs{Process: proc}).
		WithStderr(proc.Stderr()).
		WithStdout(proc.Stdout()).
		WithStdin(proc.Stdin()).
		WithStartFunctions()

	mod, err := rt.InstantiateWithConfig(ctx, contents, config)
	if err != nil {
		return err
	}

	start := mod.ExportedFunction("_start")
	if start == nil {
		return fmt.Errorf("wasm: no start function found")
	}

	if _, err := start.Call(ctx); err != nil {
		if exitErr, ok := err.(*sys.ExitError); ok {
			if exitErr.ExitCode() != 0 {
				return err
			}
			// otherwise we exited normally.
		} else {
			return fmt.Errorf("failed to call start: %+v", err)
		}
	}

	return nil
}

func NewWasmProgram(f filesystem.File) common.Program {
	return &wasmProgram{File: f}
}

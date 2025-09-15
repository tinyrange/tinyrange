// Package client provides a friendly wrapper around the low-level
// MachineService RPC that mirrors Go's standard os and exec packages.
//
// This package focuses on ergonomic helpers for common file operations
// (Open, Create, ReadFile, WriteFile, Mkdir, Stat, Chmod, Chown) and a
// lightweight exec-style API (Command/Run/Output) that streams process I/O
// over the RPC. It is intended to make remote-machine interactions feel
// like local calls while retaining explicit control over I/O and contexts.
//
// Basic usage (files):
//
//	rpc := machinepb.NewMachineServiceClient(conn)
//	c := client.New(rpc)
//	_ = c.WriteFile("/tmp/hello.txt", []byte("hi"), 0644)
//	b, _ := c.ReadFile("/tmp/hello.txt")
//
// Basic usage (exec):
//
//	out, err := c.Command("/bin/sh", "-c", "echo -n hi").Output()
//	// out == []byte("hi")
package client

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	machinepb "github.com/tinyrange/tinyrange/vibe_party/machine/proto"
)

// Client is a friendly wrapper around MachineServiceClient, exposing a Go-like
// os and exec API surface. Unless explicitly documented, semantics match the
// closest equivalent from the standard library packages os and exec.
type Client struct {
	rpc machinepb.MachineServiceClient
	// Default context for operations when caller doesn't pass one.
	// If nil, context.Background() is used.
	defaultCtx context.Context

	// pipeCap is the maximum in-memory buffer for StdoutPipe/StderrPipe
	// before backpressure blocks writes. Defaults to defaultAsyncPipeCap.
	pipeCap int
}

// New constructs a new wrapper around the given RPC client.
// The returned Client uses context.Background as its default context.
func New(rpc machinepb.MachineServiceClient) *Client {
	return &Client{rpc: rpc, defaultCtx: context.Background(), pipeCap: defaultAsyncPipeCap}
}

// WithContext returns a shallow copy of the client with defaultCtx set to ctx.
// All methods that don't accept a context will use this default.
func (c *Client) WithContext(ctx context.Context) *Client {
	nc := *c
	if ctx == nil {
		nc.defaultCtx = context.Background()
	} else {
		nc.defaultCtx = ctx
	}
	return &nc
}

// WithPipeBuffer returns a shallow copy of the client with a custom
// buffer capacity for StdoutPipe/StderrPipe. A value <= 0 resets to default.
func (c *Client) WithPipeBuffer(capacity int) *Client {
	nc := *c
	if capacity <= 0 {
		nc.pipeCap = defaultAsyncPipeCap
	} else {
		nc.pipeCap = capacity
	}
	return &nc
}

func (c *Client) ctx() context.Context {
	if c.defaultCtx != nil {
		return c.defaultCtx
	}
	return context.Background()
}

// -------------------- os-like: files and dirs --------------------

// File wraps a remote handle and implements io.Reader, io.Writer, io.Closer.
//
// Reads and writes are performed using the MachineService Read/Write streaming
// RPCs under the hood. A single call to Read maps to one server-streaming
// request and returns the first chunk; a single call to Write maps to one
// client-streaming request that sends the entire payload and then closes.
type File struct {
	c  *Client
	fd int32
	mu sync.Mutex

	// optional: for debugging/hints
	name string
	// track close state
	closed bool
}

// Fd returns the underlying remote handle id.
func (f *File) Fd() int32 { return f.fd }

// Close closes the remote handle.
func (f *File) Close() error {
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return nil
	}
	f.closed = true
	f.mu.Unlock()
	_, err := f.c.rpc.Close(f.c.ctx(), &machinepb.CloseRequest{Fd: f.fd})
	return err
}

// Read implements io.Reader by making a single-chunk streaming call.
// It returns at most len(p) bytes. If the underlying stream indicates EOF
// and no bytes are available, Read returns io.EOF.
func (f *File) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	// cancel after first chunk to avoid draining whole stream
	ctx, cancel := context.WithCancel(f.c.ctx())
	defer cancel()
	stream, err := f.c.rpc.Read(ctx, &machinepb.ReadRequest{Fd: f.fd, MaxBytes: uint32(len(p))})
	if err != nil {
		return 0, err
	}
	resp, err := stream.Recv()
	if errors.Is(err, io.EOF) {
		return 0, io.EOF
	}
	if err != nil {
		return 0, err
	}
	n := copy(p, resp.GetData())
	// best-effort: stop server from sending more
	cancel()
	if n == 0 && resp.GetEof() {
		return 0, io.EOF
	}
	return n, nil
}

// Write implements io.Writer by opening a write stream and sending once.
// The RPC returns the number of bytes accepted by the server.
func (f *File) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	stream, err := f.c.rpc.Write(f.c.ctx())
	if err != nil {
		return 0, err
	}
	if err := stream.Send(&machinepb.WriteRequest{Fd: f.fd, Data: append([]byte(nil), p...)}); err != nil {
		return 0, err
	}
	resp, err := stream.CloseAndRecv()
	if err != nil {
		return 0, err
	}
	return int(resp.GetBytesWritten()), nil
}

// Open opens a file for reading.
func (c *Client) Open(name string) (*File, error) {
	return c.OpenFile(name, os.O_RDONLY, 0)
}

// Create creates or truncates the named file.
func (c *Client) Create(name string) (*File, error) {
	return c.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0666)
}

// OpenFile mirrors os.OpenFile flag handling for a remote file.
// Note: O_APPEND is not supported by the RPC and is treated as a no-op.
func (c *Client) OpenFile(name string, flag int, perm os.FileMode) (*File, error) {
	name = filepath.Clean(name)
	req := &machinepb.OpenFileRequest{Path: name, Mode: uint32(perm)}
	// flags
	forRead := flag&os.O_WRONLY == 0 && flag&os.O_RDWR == 0
	forWrite := flag&os.O_WRONLY != 0 || flag&os.O_RDWR != 0
	req.ForRead = forRead
	req.ForWrite = forWrite
	if flag&os.O_CREATE != 0 {
		req.Create = true
	}
	if flag&os.O_TRUNC != 0 {
		req.Truncate = true
	}
	// Handle append flag if supported by server proto.
	if flag&os.O_APPEND != 0 {
		req.Append = true
	}

	of, err := c.rpc.OpenFile(c.ctx(), req)
	if err != nil {
		return nil, err
	}
	return &File{c: c, fd: of.GetFd(), name: name}, nil
}

// ReadFile reads the named file's entire contents.
func (c *Client) ReadFile(name string) ([]byte, error) {
	f, err := c.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return readAllHandle(c, f.fd)
}

// WriteFile writes data to the named file, creating it with perm if needed.
func (c *Client) WriteFile(name string, data []byte, perm os.FileMode) error {
	f, err := c.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(data)
	return err
}

// Mkdir creates a directory.
func (c *Client) Mkdir(path string, perm os.FileMode) error {
	_, err := c.rpc.Mkdir(c.ctx(), &machinepb.MkdirRequest{Path: filepath.Clean(path), Mode: uint32(perm), Parents: false})
	return err
}

// MkdirAll creates a directory and parents, like os.MkdirAll.
func (c *Client) MkdirAll(path string, perm os.FileMode) error {
	_, err := c.rpc.Mkdir(c.ctx(), &machinepb.MkdirRequest{Path: filepath.Clean(path), Mode: uint32(perm), Parents: true})
	return err
}

// fileInfo is an os.FileInfo implementation backed by StatResponse.
type fileInfo struct {
	name  string
	size  int64
	mode  os.FileMode
	mtime time.Time
	isDir bool
}

func (fi fileInfo) Name() string       { return fi.name }
func (fi fileInfo) Size() int64        { return fi.size }
func (fi fileInfo) Mode() os.FileMode  { return fi.mode }
func (fi fileInfo) ModTime() time.Time { return fi.mtime }
func (fi fileInfo) IsDir() bool        { return fi.isDir }
func (fi fileInfo) Sys() interface{}   { return nil }

// Stat returns file info following symlinks.
func (c *Client) Stat(name string) (os.FileInfo, error) {
	name = filepath.Clean(name)
	st, err := c.rpc.Stat(c.ctx(), &machinepb.StatRequest{Path: name, FollowSymlinks: true})
	if err != nil {
		return nil, err
	}
	if !st.GetExists() {
		return nil, &os.PathError{Op: "stat", Path: name, Err: os.ErrNotExist}
	}
	return fileInfo{
		name:  filepath.Base(st.GetPath()),
		size:  st.GetSize(),
		mode:  os.FileMode(st.GetMode()),
		mtime: time.Unix(st.GetMtimeSec(), st.GetMtimeNsec()),
		isDir: st.GetIsDir(),
	}, nil
}

// Lstat returns file info without following symlinks.
func (c *Client) Lstat(name string) (os.FileInfo, error) {
	name = filepath.Clean(name)
	st, err := c.rpc.Stat(c.ctx(), &machinepb.StatRequest{Path: name, FollowSymlinks: false})
	if err != nil {
		return nil, err
	}
	if !st.GetExists() {
		return nil, &os.PathError{Op: "lstat", Path: name, Err: os.ErrNotExist}
	}
	return fileInfo{
		name:  filepath.Base(st.GetPath()),
		size:  st.GetSize(),
		mode:  os.FileMode(st.GetMode()),
		mtime: time.Unix(st.GetMtimeSec(), st.GetMtimeNsec()),
		isDir: st.GetIsDir(),
	}, nil
}

// Chmod mirrors os.Chmod.
func (c *Client) Chmod(name string, mode os.FileMode) error {
	_, err := c.rpc.Chmod(c.ctx(), &machinepb.ChmodRequest{Path: filepath.Clean(name), Mode: uint32(mode)})
	return err
}

// Chown mirrors os.Chown.
func (c *Client) Chown(name string, uid, gid int) error {
	_, err := c.rpc.Chown(c.ctx(), &machinepb.ChownRequest{Path: filepath.Clean(name), Uid: int32(uid), Gid: int32(gid)})
	return err
}

// readAllHandle drains a remote handle via Read() until EOF.
func readAllHandle(c *Client, fd int32) ([]byte, error) {
	var buf bytes.Buffer
	// use a generous chunk size
	const chunk = 128 * 1024
	ctx := c.ctx()
	for {
		stream, err := c.rpc.Read(ctx, &machinepb.ReadRequest{Fd: fd, MaxBytes: chunk})
		if err != nil {
			return nil, err
		}
		// drain this server stream until EOF; server will end when source blocks/EOF
		for {
			resp, rerr := stream.Recv()
			if errors.Is(rerr, io.EOF) {
				return buf.Bytes(), nil
			}
			if rerr != nil {
				return nil, rerr
			}
			if data := resp.GetData(); len(data) > 0 {
				if _, werr := buf.Write(data); werr != nil {
					return nil, werr
				}
			}
			if resp.GetEof() {
				return buf.Bytes(), nil
			}
		}
	}
}

// -------------------- exec-like: commands --------------------

// Cmd mirrors the shape of os/exec.Cmd for a remote machine.
//
// Use Client.Command to construct a Cmd. Set optional fields (Stdin, Stdout,
// Stderr, Env, Dir, TTY) prior to Start/Run.
type Cmd struct {
	client *Client
	Path   string
	Args   []string
	Env    []string // "key=value" entries
	Dir    string
	TTY    bool

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	// internal state
	pid      int32
	stdinFD  int32
	stdoutFD int32
	stderrFD int32
	started  bool
	doneOnce sync.Once
	ioWG     sync.WaitGroup
	cancelIO context.CancelFunc

	// pipes for StdoutPipe/StderrPipe
	stdoutPipeR io.ReadCloser
	stdoutPipeW io.WriteCloser
	stderrPipeR io.ReadCloser
	stderrPipeW io.WriteCloser

	// pipeCap applied to any pipes created via StdoutPipe/StderrPipe
	pipeCap int
}

// Command creates a Cmd for the given program and args.
func (c *Client) Command(name string, args ...string) *Cmd {
	return &Cmd{client: c, Path: name, Args: append([]string(nil), args...), pipeCap: c.pipeCap}
}

// CommandContext sets a default context via WithContext and returns a Cmd.
func (c *Client) CommandContext(ctx context.Context, name string, args ...string) *Cmd {
	return c.WithContext(ctx).Command(name, args...)
}

// StdoutPipe returns a pipe that will receive stdout data after Start.
// The caller must call Start before first read and must call Wait to
// release underlying resources.
func (cmd *Cmd) StdoutPipe() (io.ReadCloser, error) {
	if cmd.Stdout != nil {
		return nil, fmt.Errorf("exec: Stdout already set")
	}
	r, w := newAsyncPipeWithCap(cmd.pipeCap)
	cmd.Stdout = w
	cmd.stdoutPipeR = r
	cmd.stdoutPipeW = w
	return r, nil
}

// StderrPipe returns a pipe that will receive stderr data after Start.
// The caller must call Start before first read and must call Wait to
// release underlying resources.
func (cmd *Cmd) StderrPipe() (io.ReadCloser, error) {
	if cmd.Stderr != nil {
		return nil, fmt.Errorf("exec: Stderr already set")
	}
	r, w := newAsyncPipeWithCap(cmd.pipeCap)
	cmd.Stderr = w
	cmd.stderrPipeR = r
	cmd.stderrPipeW = w
	return r, nil
}

// Start starts the remote process and begins bridging stdio.
func (cmd *Cmd) Start() error {
	if cmd.started {
		return fmt.Errorf("exec: already started")
	}
	envMap := map[string]string{}
	for _, kv := range cmd.Env {
		if i := strings.IndexByte(kv, '='); i > 0 {
			envMap[kv[:i]] = kv[i+1:]
		}
	}
	req := &machinepb.ExecRequest{
		Command:    cmd.Path,
		Args:       append([]string(nil), cmd.Args...),
		Env:        envMap,
		WorkingDir: cmd.Dir,
		Tty:        cmd.TTY,
	}
	resp, err := cmd.client.rpc.Exec(cmd.client.ctx(), req)
	if err != nil {
		return err
	}
	cmd.pid = resp.GetPid()
	cmd.stdinFD = resp.GetStdinFd()
	cmd.stdoutFD = resp.GetStdoutFd()
	cmd.stderrFD = resp.GetStderrFd()
	cmd.started = true

	ioCtx, cancel := context.WithCancel(cmd.client.ctx())
	cmd.cancelIO = cancel

	// Bridge stdin
	if cmd.Stdin != nil && cmd.stdinFD != 0 {
		cmd.ioWG.Add(1)
		go func(r io.Reader, fd int32) {
			defer cmd.ioWG.Done()
			_ = copyToRemote(ioCtx, cmd.client.rpc, fd, r)
			// ignore errors; process may exit early
		}(cmd.Stdin, cmd.stdinFD)
	}
	// Bridge stdout
	if cmd.Stdout != nil && cmd.stdoutFD != 0 {
		cmd.ioWG.Add(1)
		go func(w io.Writer, fd int32, closePipe func()) {
			defer cmd.ioWG.Done()
			_ = copyFromRemote(ioCtx, cmd.client.rpc, fd, w)
			if closePipe != nil {
				closePipe()
			}
		}(cmd.Stdout, cmd.stdoutFD, func() {
			if cmd.stdoutPipeW != nil {
				_ = cmd.stdoutPipeW.Close()
			}
		})
	}
	// Bridge stderr unless in TTY mode (stderrFD may be 0)
	if cmd.Stderr != nil && cmd.stderrFD != 0 {
		cmd.ioWG.Add(1)
		go func(w io.Writer, fd int32, closePipe func()) {
			defer cmd.ioWG.Done()
			_ = copyFromRemote(ioCtx, cmd.client.rpc, fd, w)
			if closePipe != nil {
				closePipe()
			}
		}(cmd.Stderr, cmd.stderrFD, func() {
			if cmd.stderrPipeW != nil {
				_ = cmd.stderrPipeW.Close()
			}
		})
	}
	return nil
}

// Wait waits for the process to exit. Non-zero exit codes return *ExitError.
func (cmd *Cmd) Wait() error {
	if !cmd.started {
		return fmt.Errorf("exec: not started")
	}
	// wait for process
	wr, err := cmd.client.rpc.WaitProcess(cmd.client.ctx(), &machinepb.WaitProcessRequest{Pid: cmd.pid})
	// proactively close local pipe writers to unblock any pending writes
	if cmd.stdoutPipeW != nil {
		_ = cmd.stdoutPipeW.Close()
	}
	if cmd.stderrPipeW != nil {
		_ = cmd.stderrPipeW.Close()
	}
	// stop IO bridges
	if cmd.cancelIO != nil {
		cmd.cancelIO()
	}
	cmd.ioWG.Wait()

	// best-effort: close remote stdio handles
	if cmd.stdinFD != 0 {
		_, _ = cmd.client.rpc.Close(cmd.client.ctx(), &machinepb.CloseRequest{Fd: cmd.stdinFD})
	}
	if cmd.stdoutFD != 0 {
		_, _ = cmd.client.rpc.Close(cmd.client.ctx(), &machinepb.CloseRequest{Fd: cmd.stdoutFD})
	}
	if cmd.stderrFD != 0 {
		_, _ = cmd.client.rpc.Close(cmd.client.ctx(), &machinepb.CloseRequest{Fd: cmd.stderrFD})
	}

	if err != nil {
		return err
	}
	if code := int(wr.GetExitCode()); code != 0 {
		return &ExitError{Code: code, Pid: int(cmd.pid)}
	}
	return nil
}

// Run starts and waits for completion.
func (cmd *Cmd) Run() error {
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Wait()
}

// Output runs the command and returns its standard output.
func (cmd *Cmd) Output() ([]byte, error) {
	if cmd.Stdout != nil {
		return nil, fmt.Errorf("exec: Stdout already set")
	}
	var buf bytes.Buffer
	cmd.Stdout = &buf
	err := cmd.Run()
	return buf.Bytes(), err
}

// CombinedOutput runs the command and returns combined stdout and stderr.
func (cmd *Cmd) CombinedOutput() ([]byte, error) {
	if cmd.Stdout != nil || cmd.Stderr != nil {
		return nil, fmt.Errorf("exec: Stdout/Stderr already set")
	}
	var buf bytes.Buffer
	// concurrent safe writer
	mw := &lockedWriter{W: &buf}
	cmd.Stdout = mw
	// If stderrFD is 0 (TTY mode), output is already combined into stdout.
	cmd.Stderr = mw
	err := cmd.Run()
	return buf.Bytes(), err
}

// ExitError mirrors os/exec.ExitError minimally.
type ExitError struct {
	Code int
	Pid  int
}

func (e *ExitError) Error() string { return fmt.Sprintf("exit status %d", e.Code) }

// Utilities

// copyFromRemote reads chunks from remote fd and writes to w until EOF.
func copyFromRemote(ctx context.Context, rpc machinepb.MachineServiceClient, fd int32, w io.Writer) error {
	// One long-lived stream that the server will drive until EOF.
	stream, err := rpc.Read(ctx, &machinepb.ReadRequest{Fd: fd, MaxBytes: 128 * 1024})
	if err != nil {
		return err
	}
	for {
		resp, rerr := stream.Recv()
		if errors.Is(rerr, io.EOF) {
			return nil
		}
		if rerr != nil {
			return rerr
		}
		if data := resp.GetData(); len(data) > 0 {
			if _, werr := w.Write(data); werr != nil {
				return werr
			}
		}
		if resp.GetEof() {
			return nil
		}
	}
}

// copyToRemote reads from r and sends to remote fd via a single Write stream.
func copyToRemote(ctx context.Context, rpc machinepb.MachineServiceClient, fd int32, r io.Reader) error {
	stream, err := rpc.Write(ctx)
	if err != nil {
		return err
	}
	buf := make([]byte, 32*1024)
	for {
		n, rerr := r.Read(buf)
		if n > 0 {
			if err := stream.Send(&machinepb.WriteRequest{Fd: fd, Data: buf[:n]}); err != nil {
				_ = stream.CloseSend()
				return err
			}
		}
		if rerr != nil {
			if errors.Is(rerr, io.EOF) {
				_, cerr := stream.CloseAndRecv()
				return cerr
			}
			_ = stream.CloseSend()
			return rerr
		}
	}
}

type lockedWriter struct {
	mu sync.Mutex
	W  io.Writer
}

func (lw *lockedWriter) Write(p []byte) (int, error) {
	lw.mu.Lock()
	defer lw.mu.Unlock()
	return lw.W.Write(p)
}

// asyncPipe provides a non-blocking writer and a blocking reader backed by an
// in-memory buffer. It prevents writer-side deadlocks when the reader is not
// yet consuming, which can occur with io.Pipe.
type asyncPipe struct {
	mu           sync.Mutex
	cond         *sync.Cond
	buf          bytes.Buffer
	closed       bool // writer closed
	readerClosed bool // reader closed
	capacity     int  // max buffered bytes before writer blocks
}

type asyncReader struct{ p *asyncPipe }
type asyncWriter struct{ p *asyncPipe }

const defaultAsyncPipeCap = 256 * 1024 // 256KiB backpressure

func newAsyncPipeWithCap(capacity int) (io.ReadCloser, io.WriteCloser) {
	if capacity <= 0 {
		capacity = defaultAsyncPipeCap
	}
	ap := &asyncPipe{capacity: capacity}
	ap.cond = sync.NewCond(&ap.mu)
	return &asyncReader{p: ap}, &asyncWriter{p: ap}
}

func (w *asyncWriter) Write(p []byte) (int, error) {
	a := w.p
	a.mu.Lock()
	if a.closed || a.readerClosed {
		a.mu.Unlock()
		return 0, io.ErrClosedPipe
	}
	// Write with backpressure in chunks.
	written := 0
	for written < len(p) {
		for a.buf.Len() >= a.capacity && !a.closed && !a.readerClosed {
			a.cond.Wait()
		}
		if a.closed || a.readerClosed {
			a.mu.Unlock()
			if written == 0 {
				return 0, io.ErrClosedPipe
			}
			return written, nil
		}
		space := a.capacity - a.buf.Len()
		if space <= 0 { // should not happen due to wait above
			space = 1
		}
		n := len(p) - written
		if n > space {
			n = space
		}
		_, _ = a.buf.Write(p[written : written+n])
		written += n
		a.cond.Broadcast()
	}
	a.mu.Unlock()
	return written, nil
}
func (w *asyncWriter) Close() error {
	a := w.p
	a.mu.Lock()
	a.closed = true
	a.cond.Broadcast()
	a.mu.Unlock()
	return nil
}

func (r *asyncReader) Read(p []byte) (int, error) {
	a := r.p
	a.mu.Lock()
	for a.buf.Len() == 0 && !a.closed {
		a.cond.Wait()
	}
	if a.buf.Len() == 0 && (a.closed || a.readerClosed) {
		a.mu.Unlock()
		return 0, io.EOF
	}
	n, _ := a.buf.Read(p)
	a.mu.Unlock()
	return n, nil
}
func (r *asyncReader) Close() error {
	a := r.p
	a.mu.Lock()
	a.readerClosed = true
	a.cond.Broadcast()
	a.mu.Unlock()
	return nil
}

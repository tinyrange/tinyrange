package machine

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	machinepb "github.com/tinyrange/tinyrange/vibe_party/machine/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// ---- Test stream fakes ----
type readStream struct {
	tb   testing.TB
	bufs [][]byte
}

func (s *readStream) Send(resp *machinepb.ReadResponse) error {
	s.bufs = append(s.bufs, append([]byte(nil), resp.GetData()...))
	return nil
}
func (s *readStream) SetHeader(md metadata.MD) error  { return nil }
func (s *readStream) SendHeader(md metadata.MD) error { return nil }
func (s *readStream) SetTrailer(md metadata.MD)       {}
func (s *readStream) Context() context.Context        { return context.Background() }
func (s *readStream) SendMsg(m interface{}) error     { return nil }
func (s *readStream) RecvMsg(m interface{}) error     { return nil }

type writeStream struct {
	tb     testing.TB
	msgs   []*machinepb.WriteRequest
	idx    int
	closed bool
}

func (s *writeStream) Recv() (*machinepb.WriteRequest, error) {
	if s.idx >= len(s.msgs) {
		return nil, io.EOF
	}
	m := s.msgs[s.idx]
	s.idx++
	return m, nil
}
func (s *writeStream) SendAndClose(resp *machinepb.WriteResponse) error { s.closed = true; return nil }
func (s *writeStream) SetHeader(md metadata.MD) error                   { return nil }
func (s *writeStream) SendHeader(md metadata.MD) error                  { return nil }
func (s *writeStream) SetTrailer(md metadata.MD)                        {}
func (s *writeStream) Context() context.Context                         { return context.Background() }
func (s *writeStream) SendMsg(m interface{}) error                      { return nil }
func (s *writeStream) RecvMsg(m interface{}) error                      { return nil }

// Ensure fakes satisfy gRPC generic interfaces at compile time.
var _ grpc.ServerStreamingServer[machinepb.ReadResponse] = (*readStream)(nil)
var _ grpc.ClientStreamingServer[machinepb.WriteRequest, machinepb.WriteResponse] = (*writeStream)(nil)

func TestOpenFileWriteReadClose(t *testing.T) {
	s := NewLocalMachineServer()
	dir := t.TempDir()
	p := filepath.Join(dir, "hello.txt")

	// Open for write (create)
	of, err := s.OpenFile(context.Background(), &machinepb.OpenFileRequest{
		Path: p, ForWrite: true, Create: true, Truncate: true, Mode: 0644,
	})
	if err != nil {
		t.Fatalf("OpenFile write: %v", err)
	}

	// Write two chunks via streaming
	ws := &writeStream{tb: t, msgs: []*machinepb.WriteRequest{
		{Fd: of.Fd, Data: []byte("hello ")},
		{Fd: of.Fd, Data: []byte("world")},
	}}
	if err := s.Write(ws); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if !ws.closed {
		t.Fatalf("Write stream not closed")
	}

	// Close handle
	if _, err := s.Close(context.Background(), &machinepb.CloseRequest{Fd: of.Fd}); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Open for read and stream contents
	ofr, err := s.OpenFile(context.Background(), &machinepb.OpenFileRequest{
		Path: p, ForRead: true,
	})
	if err != nil {
		t.Fatalf("OpenFile read: %v", err)
	}

	rs := &readStream{tb: t}
	if err := s.Read(&machinepb.ReadRequest{Fd: ofr.Fd, MaxBytes: 4}, rs); err != nil {
		t.Fatalf("Read: %v", err)
	}
	var got strings.Builder
	for _, b := range rs.bufs {
		got.Write(b)
	}
	if got.String() != "hello world" {
		t.Fatalf("unexpected read %q", got.String())
	}
}

func TestExecNonTTYCapturesStdoutAndExit(t *testing.T) {
	s := NewLocalMachineServer()
	shell := "/bin/sh"
	if runtime.GOOS == "windows" {
		t.Skip("skip on windows")
	}
	cmd := "-c"
	of, err := s.Exec(context.Background(), &machinepb.ExecRequest{
		Command: shell, Args: []string{cmd, "echo -n hello"}, Tty: false,
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	rs := &readStream{tb: t}
	if err := s.Read(&machinepb.ReadRequest{Fd: of.StdoutFd, MaxBytes: 32}, rs); err != nil {
		t.Fatalf("Read stdout: %v", err)
	}
	var out strings.Builder
	for _, b := range rs.bufs {
		out.Write(b)
	}
	if out.String() != "hello" {
		t.Fatalf("unexpected stdout %q", out.String())
	}

	// Wait should return 0
	wr, err := s.WaitProcess(context.Background(), &machinepb.WaitProcessRequest{Pid: of.Pid})
	if err != nil {
		t.Fatalf("WaitProcess: %v", err)
	}
	if wr.ExitCode != 0 {
		t.Fatalf("unexpected exit code %d", wr.ExitCode)
	}
}

func TestExecTTYCombinesStdoutAndStderr(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skip on windows")
	}
	s := NewLocalMachineServer()
	shell := "/bin/sh"
	cmd := "-c"
	of, err := s.Exec(context.Background(), &machinepb.ExecRequest{
		Command: shell, Args: []string{cmd, "echo -n out; echo -n err 1>&2"}, Tty: true,
	})
	if err != nil {
		t.Fatalf("Exec tty: %v", err)
	}
	if of.StderrFd != 0 {
		t.Fatalf("expected no stderr fd in TTY mode, got %d", of.StderrFd)
	}
	rs := &readStream{tb: t}
	if err := s.Read(&machinepb.ReadRequest{Fd: of.StdoutFd, MaxBytes: 64}, rs); err != nil {
		t.Fatalf("Read combined: %v", err)
	}
	var out strings.Builder
	for _, b := range rs.bufs {
		out.Write(b)
	}
	if out.String() != "outerr" {
		t.Fatalf("unexpected combined %q", out.String())
	}
	// Close stdin and wait
	_, _ = s.Close(context.Background(), &machinepb.CloseRequest{Fd: of.StdinFd})
	wr, err := s.WaitProcess(context.Background(), &machinepb.WaitProcessRequest{Pid: of.Pid})
	if err != nil {
		t.Fatalf("WaitProcess: %v", err)
	}
	if wr.ExitCode != 0 {
		t.Fatalf("unexpected exit code %d", wr.ExitCode)
	}
}

func TestMkdirStatChmod(t *testing.T) {
	// Chmod and Stat semantics differ on Windows; skip there.
	if runtime.GOOS == "windows" {
		t.Skip("skip on windows")
	}
	s := NewLocalMachineServer()
	dir := t.TempDir()
	nested := filepath.Join(dir, "a", "b", "c")
	// mkdir -p
	if _, err := s.Mkdir(context.Background(), &machinepb.MkdirRequest{Path: nested, Mode: 0755, Parents: true}); err != nil {
		t.Fatalf("Mkdir -p: %v", err)
	}
	st, err := s.Stat(context.Background(), &machinepb.StatRequest{Path: nested, FollowSymlinks: true})
	if err != nil || !st.Exists || !st.IsDir {
		t.Fatalf("Stat dir: %+v err=%v", st, err)
	}

	// non-existent
	st2, err := s.Stat(context.Background(), &machinepb.StatRequest{Path: filepath.Join(dir, "nope"), FollowSymlinks: true})
	if err != nil || st2.Exists {
		t.Fatalf("Stat nonexist: %+v err=%v", st2, err)
	}

	// Create file and chmod
	filePath := filepath.Join(dir, "f.txt")
	of, err := s.OpenFile(context.Background(), &machinepb.OpenFileRequest{Path: filePath, ForWrite: true, Create: true, Truncate: true, Mode: 0644})
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	// Close it
	_, _ = s.Close(context.Background(), &machinepb.CloseRequest{Fd: of.Fd})

	if _, err := s.Chmod(context.Background(), &machinepb.ChmodRequest{Path: filePath, Mode: 0600}); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	st3, err := s.Stat(context.Background(), &machinepb.StatRequest{Path: filePath, FollowSymlinks: true})
	if err != nil {
		t.Fatalf("Stat file: %v", err)
	}
	if st3.Mode&0600 != 0600 {
		t.Fatalf("expected mode to include 0600, got %o", st3.Mode)
	}
}

func TestStatSymlinkFollowBehavior(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skip symlink tests on windows")
	}
	s := NewLocalMachineServer()
	dir := t.TempDir()

	// Create a file and a symlink to it
	target := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(target, []byte("x"), 0644); err != nil {
		t.Fatalf("write target: %v", err)
	}
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	// Lstat (no follow): exists true, is_dir false
	st1, err := s.Stat(context.Background(), &machinepb.StatRequest{Path: link, FollowSymlinks: false})
	if err != nil {
		t.Fatalf("lstat symlink: %v", err)
	}
	if !st1.Exists || st1.IsDir {
		t.Fatalf("unexpected lstat: %+v", st1)
	}

	// Stat (follow): exists true, is_dir false, size 1
	st2, err := s.Stat(context.Background(), &machinepb.StatRequest{Path: link, FollowSymlinks: true})
	if err != nil {
		t.Fatalf("stat symlink: %v", err)
	}
	if !st2.Exists || st2.IsDir {
		t.Fatalf("unexpected stat: %+v", st2)
	}
	if st2.Size != 1 {
		t.Fatalf("expected size 1, got %d", st2.Size)
	}

	// Directory target
	tdir := filepath.Join(dir, "d")
	if err := os.MkdirAll(tdir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	dlink := filepath.Join(dir, "dlink")
	if err := os.Symlink(tdir, dlink); err != nil {
		t.Fatalf("symlink dir: %v", err)
	}

	st3, err := s.Stat(context.Background(), &machinepb.StatRequest{Path: dlink, FollowSymlinks: false})
	if err != nil {
		t.Fatalf("lstat dlink: %v", err)
	}
	if !st3.Exists || st3.IsDir {
		t.Fatalf("unexpected lstat dir link: %+v", st3)
	}
	st4, err := s.Stat(context.Background(), &machinepb.StatRequest{Path: dlink, FollowSymlinks: true})
	if err != nil {
		t.Fatalf("stat dlink: %v", err)
	}
	if !st4.Exists || !st4.IsDir {
		t.Fatalf("unexpected stat dir link: %+v", st4)
	}

	// Broken link
	broken := filepath.Join(dir, "broken")
	if err := os.Symlink(filepath.Join(dir, "no_such"), broken); err != nil {
		t.Fatalf("symlink broken: %v", err)
	}
	st5, err := s.Stat(context.Background(), &machinepb.StatRequest{Path: broken, FollowSymlinks: false})
	if err != nil || !st5.Exists {
		t.Fatalf("lstat broken: %+v err=%v", st5, err)
	}
	st6, err := s.Stat(context.Background(), &machinepb.StatRequest{Path: broken, FollowSymlinks: true})
	if err != nil || st6.Exists {
		t.Fatalf("stat broken: %+v err=%v", st6, err)
	}
}

func TestChownSameIDsNoop(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skip on windows")
	}
	s := NewLocalMachineServer()
	dir := t.TempDir()
	p := filepath.Join(dir, "file")
	if err := os.WriteFile(p, []byte("x"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Query current uid/gid from file owner via Stat (no uid/gid, so we use os.FileInfo.Sys when possible) is complex.
	// Instead, attempt to chown to our current user/group using environment.
	// Parse from os.Getuid/Getgid via syscall where available using /proc/self status fallback not needed in tests.
	// We will accept EPERM if the platform forbids even no-op chown for non-root.
	uid := os.Getuid()
	gid := os.Getgid()

	resp, err := s.Chown(context.Background(), &machinepb.ChownRequest{Path: p, Uid: int32(uid), Gid: int32(gid)})
	if err != nil {
		if resp != nil && resp.Success {
			t.Fatalf("chown returned error but success=true")
		}
		// EPERM or EINVAL acceptable on restricted environments
		t.Logf("chown not permitted: %v", err)
	} else {
		if resp == nil || !resp.Success {
			t.Fatalf("expected success from chown")
		}
	}
}

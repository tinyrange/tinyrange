package client

import (
	"context"
	"io"
	"net"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	machinepkg "github.com/tinyrange/tinyrange/vibe_party/machine"
	machinepb "github.com/tinyrange/tinyrange/vibe_party/machine/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

func newTestClient(t *testing.T) (*Client, func()) {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	machinepb.RegisterMachineServiceServer(srv, machinepkg.NewLocalMachineServer())
	done := make(chan struct{})
	go func() { _ = srv.Serve(lis); close(done) }()

	dialer := func(ctx context.Context, s string) (netConn net.Conn, err error) {
		return lis.Dial()
	}

	ctx := context.Background()
	conn, err := grpc.DialContext(ctx, "bufnet", grpc.WithContextDialer(dialer), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	rpc := machinepb.NewMachineServiceClient(conn)
	c := New(rpc)

	cleanup := func() {
		_ = conn.Close()
		srv.Stop()
		_ = lis.Close()
		<-done
	}
	return c, cleanup
}

func TestReadWriteFile(t *testing.T) {
	c, closeFn := newTestClient(t)
	defer closeFn()

	dir := t.TempDir()
	p := filepath.Join(dir, "hello.txt")

	if err := c.WriteFile(p, []byte("hello world"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := c.ReadFile(p)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "hello world" {
		t.Fatalf("unexpected content %q", string(got))
	}
}

func TestOpenFileAPIs(t *testing.T) {
	c, closeFn := newTestClient(t)
	defer closeFn()
	dir := t.TempDir()
	p := filepath.Join(dir, "data.bin")

	f, err := c.Create(p)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer f.Close()
	if _, err := f.Write([]byte("abc")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	_ = f.Close()

	rf, err := c.Open(p)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer rf.Close()
	buf := make([]byte, 2)
	n, err := rf.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("Read: %v", err)
	}
	if string(buf[:n]) != "ab" {
		t.Fatalf("unexpected read %q", string(buf[:n]))
	}
}

func TestExecOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skip on windows")
	}
	c, closeFn := newTestClient(t)
	defer closeFn()
	out, err := c.Command("/bin/sh", "-c", "echo -n hello").Output()
	if err != nil {
		t.Fatalf("Output: %v", err)
	}
	if string(out) != "hello" {
		t.Fatalf("unexpected output %q", string(out))
	}
}

func TestExecCombinedOutputTTY(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skip on windows")
	}
	c, closeFn := newTestClient(t)
	defer closeFn()
	cmd := c.Command("/bin/sh", "-c", "echo -n out; echo -n err 1>&2")
	cmd.TTY = true
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CombinedOutput: %v", err)
	}
	if string(out) != "outerr" {
		t.Fatalf("unexpected combined %q", string(out))
	}
}

func TestExecExitError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skip on windows")
	}
	c, closeFn := newTestClient(t)
	defer closeFn()
	err := c.Command("/bin/sh", "-c", "exit 42").Run()
	if err == nil {
		t.Fatalf("expected error")
	}
	ee, ok := err.(*ExitError)
	if !ok {
		t.Fatalf("expected ExitError, got %T", err)
	}
	if ee.Code != 42 {
		t.Fatalf("unexpected code %d", ee.Code)
	}
}

func TestMkdirStatChmod(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod/stat semantics differ on windows")
	}
	c, closeFn := newTestClient(t)
	defer closeFn()
	dir := t.TempDir()
	nested := filepath.Join(dir, "a", "b", "c")
	if err := c.MkdirAll(nested, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	fi, err := c.Stat(nested)
	if err != nil || !fi.IsDir() {
		t.Fatalf("Stat dir: %+v err=%v", fi, err)
	}

	f := filepath.Join(dir, "f.txt")
	if err := c.WriteFile(f, []byte("x"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := c.Chmod(f, 0600); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	fi2, err := c.Stat(f)
	if err != nil {
		t.Fatalf("Stat file: %v", err)
	}
	if fi2.Mode()&0600 != 0600 {
		t.Fatalf("unexpected mode %o", fi2.Mode())
	}
}

func TestStdoutAndStderrPipes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skip on windows")
	}
	c, closeFn := newTestClient(t)
	defer closeFn()
	cmd := c.Command("/bin/sh", "-c", "printf out; printf err 1>&2")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe: %v", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatalf("StderrPipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	bout, berr := io.ReadAll(stdout)
	if err := cmd.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if berr != nil {
		t.Fatalf("read stdout: %v", berr)
	}
	berrdata, err2 := io.ReadAll(stderr)
	if err2 != nil {
		t.Fatalf("read stderr: %v", err2)
	}
	if string(bout) != "out" || string(berrdata) != "err" {
		t.Fatalf("unexpected stdout/stderr %q / %q", string(bout), string(berrdata))
	}
}

func TestCombinedOutputNonTTYContainsBoth(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skip on windows")
	}
	c, closeFn := newTestClient(t)
	defer closeFn()
	out, err := c.Command("/bin/sh", "-c", "echo -n out; echo -n err 1>&2").CombinedOutput()
	if err != nil {
		t.Fatalf("CombinedOutput: %v", err)
	}
	s := string(out)
	if !(strings.Contains(s, "out") && strings.Contains(s, "err")) {
		t.Fatalf("expected both out and err, got %q", s)
	}
}

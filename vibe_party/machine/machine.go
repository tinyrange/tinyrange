package machine

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"

	machinepb "github.com/tinyrange/tinyrange/vibe_party/machine/proto"
	"google.golang.org/grpc"
)

// LocalMachineServer implements MachineService against the local machine.
// It keeps an in-process handle table mapping small int IDs to open resources
// (files, sockets, and process pipes). IDs are not OS FDs and are scoped to this process.
type LocalMachineServer struct {
	machinepb.UnimplementedMachineServiceServer

	mu      sync.RWMutex
	nextID  int32
	handles map[int32]interface{}

	// Track processes by PID for signal/wait.
	procs map[int32]*exec.Cmd
}

// NewLocalMachineServer constructs a new server instance.
func NewLocalMachineServer() *LocalMachineServer {
	return &LocalMachineServer{
		nextID:  100, // reserve small range for future use
		handles: make(map[int32]interface{}),
		procs:   make(map[int32]*exec.Cmd),
	}
}

// isLoopbackAddr returns true if addr is empty, localhost, 127.0.0.1, or ::1.
func isLoopbackAddr(addr string) bool {
	a := strings.TrimSpace(addr)
	if a == "" || a == "localhost" {
		return true
	}
	ip := net.ParseIP(a)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}

// getHandle fetches a handle in a threadsafe way.
func (s *LocalMachineServer) getHandle(id int32) (interface{}, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	h, ok := s.handles[id]
	return h, ok
}

func (s *LocalMachineServer) putHandle(h interface{}) int32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	id := s.nextID
	s.handles[id] = h
	return id
}

func (s *LocalMachineServer) dropHandle(id int32) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.handles[id]; ok {
		delete(s.handles, id)
		return true
	}
	return false
}

// OpenFile implements opening a local file with provided flags.
func (s *LocalMachineServer) OpenFile(ctx context.Context, req *machinepb.OpenFileRequest) (*machinepb.OpenFileResponse, error) {
	if req.GetPath() == "" {
		return nil, fmt.Errorf("path is required")
	}

	// Security: restrict to local filesystem. Normalize path; optionally could enforce a base dir.
	path := filepath.Clean(req.GetPath())

	// Derive flags
	var flags int
	switch {
	case req.GetForRead() && req.GetForWrite():
		flags = os.O_RDWR
	case req.GetForWrite():
		flags = os.O_WRONLY
	default:
		flags = os.O_RDONLY
	}
	if req.GetCreate() {
		flags |= os.O_CREATE
	}
	if req.GetTruncate() && (flags&os.O_WRONLY != 0 || flags&os.O_RDWR != 0) {
		flags |= os.O_TRUNC
	}
	if req.GetAppend() {
		flags |= os.O_APPEND
	}

	f, err := os.OpenFile(path, flags, os.FileMode(req.GetMode()))
	if err != nil {
		return nil, err
	}

	id := s.putHandle(f)
	return &machinepb.OpenFileResponse{Fd: id}, nil
}

// Read streams data from a handle until EOF or no more data.
func (s *LocalMachineServer) Read(req *machinepb.ReadRequest, stream grpc.ServerStreamingServer[machinepb.ReadResponse]) error {
	if req == nil {
		return fmt.Errorf("nil request")
	}
	max := req.GetMaxBytes()
	if max == 0 {
		max = 64 * 1024
	}

	// Try io.Reader first
	if h, ok := s.getHandle(req.GetFd()); ok {
		r, ok := h.(io.Reader)
		if !ok {
			return fmt.Errorf("fd %d not readable", req.GetFd())
		}
		buf := make([]byte, max)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				resp := machinepb.ReadResponse{Data: append([]byte(nil), buf[:n]...)}
				// EOF determined when err == io.EOF and n == 0 after sending last bytes
				if err == io.EOF {
					resp.Eof = true
				}
				if sErr := stream.Send(&resp); sErr != nil {
					return sErr
				}
			}
			if errors.Is(err, io.EOF) {
				return nil
			}
			if err != nil {
				return err
			}
		}
	}

	// UDPConn supports Read as well; the above covers it. If not found:
	return fmt.Errorf("fd %d not found or not readable", req.GetFd())
}

// Write consumes a stream of WriteRequest and writes to the identified handle.
func (s *LocalMachineServer) Write(stream grpc.ClientStreamingServer[machinepb.WriteRequest, machinepb.WriteResponse]) error {
	var total uint64
	var target io.Writer
	var targetID int32

	for {
		req, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			// done
			return stream.SendAndClose(&machinepb.WriteResponse{BytesWritten: total})
		}
		if err != nil {
			return err
		}

		if target == nil {
			if h, ok := s.getHandle(req.GetFd()); ok {
				w, ok := h.(io.Writer)
				if !ok {
					return fmt.Errorf("fd %d not writable", req.GetFd())
				}
				target = w
				targetID = req.GetFd()
			} else {
				return fmt.Errorf("fd %d not found or not writable", req.GetFd())
			}
		} else if req.GetFd() != targetID {
			return fmt.Errorf("mixed fd in write stream: %d then %d", targetID, req.GetFd())
		}

		n, werr := target.Write(req.GetData())
		if n > 0 {
			total += uint64(n)
		}
		if werr != nil {
			return werr
		}
	}
}

// Close closes a handle if possible and drops it from the table.
func (s *LocalMachineServer) Close(ctx context.Context, req *machinepb.CloseRequest) (*machinepb.CloseResponse, error) {
	s.mu.RLock()
	h, ok := s.handles[req.GetFd()]
	s.mu.RUnlock()
	if !ok {
		return &machinepb.CloseResponse{Success: false}, fmt.Errorf("fd %d not found", req.GetFd())
	}
	// Try io.Closer
	var cerr error
	if c, ok := h.(io.Closer); ok {
		cerr = c.Close()
	}
	s.dropHandle(req.GetFd())
	return &machinepb.CloseResponse{Success: cerr == nil}, cerr
}

// Exec starts a local process with pipes for stdio.
func (s *LocalMachineServer) Exec(ctx context.Context, req *machinepb.ExecRequest) (*machinepb.ExecResponse, error) {
	if strings.TrimSpace(req.GetCommand()) == "" {
		return nil, fmt.Errorf("command is required")
	}

	// Important: do NOT bind child lifetime to the RPC context.
	// The RPC context ends when this method returns, which would kill the child
	// if we used exec.CommandContext. Use a detached command and manage it via
	// SignalProcess/WaitProcess instead.
	cmd := exec.Command(req.GetCommand(), req.GetArgs()...)
	if d := strings.TrimSpace(req.GetWorkingDir()); d != "" {
		cmd.Dir = d
	}
	if len(req.GetEnv()) > 0 {
		env := os.Environ()
		for k, v := range req.GetEnv() {
			if k == "" {
				continue
			}
			env = append(env, k+"="+v)
		}
		cmd.Env = env
	}

	var (
		stdoutID, stderrID, stdinID int32
	)

	// If TTY requested, combine stdout and stderr into a single pipe; not a real PTY.
	if req.GetTty() {
		// Delegate to OS-specific TTY implementation (linux) or fallback.
		return s.execWithTTY(ctx, cmd)
	} else {
		// Non-TTY: separate pipes
		out, err := cmd.StdoutPipe()
		if err != nil {
			return nil, err
		}
		errOut, err := cmd.StderrPipe()
		if err != nil {
			return nil, err
		}
		in, err := cmd.StdinPipe()
		if err != nil {
			return nil, err
		}

		if err := cmd.Start(); err != nil {
			return nil, err
		}

		// Wrap to ensure buffering reasonable for Read
		sout := bufio.NewReader(out)
		serr := bufio.NewReader(errOut)

		stdinID = s.putHandle(in)
		stdoutID = s.putHandle(sout)
		stderrID = s.putHandle(serr)
	}

	// Track process by PID for signaling/wait
	pid := int32(cmd.Process.Pid)
	s.mu.Lock()
	s.procs[pid] = cmd
	s.mu.Unlock()

	return &machinepb.ExecResponse{
		Pid:      pid,
		StdinFd:  stdinID,
		StdoutFd: stdoutID,
		StderrFd: stderrID,
	}, nil
}

// SignalProcess sends a signal to the OS process.
func (s *LocalMachineServer) SignalProcess(ctx context.Context, req *machinepb.SignalProcessRequest) (*machinepb.SignalProcessResponse, error) {
	pid := req.GetPid()
	s.mu.RLock()
	cmd := s.procs[pid]
	s.mu.RUnlock()
	if cmd == nil || cmd.Process == nil {
		// Attempt to look up anyway
		p, err := os.FindProcess(int(pid))
		if err != nil {
			return &machinepb.SignalProcessResponse{Success: false}, err
		}
		cmd = &exec.Cmd{Process: p}
	}

	sigNum := int(req.GetSignal())
	// Map numeric signal to os.Signal; on Windows only SIGKILL-like is supported.
	var sig os.Signal
	if runtime.GOOS == "windows" {
		// Windows doesn't support Unix signals; use Kill for 9 and Term for 15 otherwise.
		if sigNum == 9 {
			sig = os.Kill
		} else {
			sig = os.Interrupt
		}
	} else {
		// Best-effort mapping using syscall.Signal
		sig = syscall.Signal(sigNum)
	}

	err := cmd.Process.Signal(sig)
	return &machinepb.SignalProcessResponse{Success: err == nil}, err
}

// WaitProcess waits for the process to exit and returns its exit code.
func (s *LocalMachineServer) WaitProcess(ctx context.Context, req *machinepb.WaitProcessRequest) (*machinepb.WaitProcessResponse, error) {
	pid := req.GetPid()
	s.mu.RLock()
	cmd := s.procs[pid]
	s.mu.RUnlock()
	if cmd == nil || cmd.Process == nil {
		// As a fallback, try waiting using os.FindProcess + Wait on Unix via syscall
		// But exec.Cmd provides better Wait; if unknown, return not found.
		return nil, fmt.Errorf("process %d not tracked", pid)
	}

	// Wait with context: if context is done, return.
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case err := <-done:
		// Remove from map
		s.mu.Lock()
		delete(s.procs, pid)
		s.mu.Unlock()
		if err == nil {
			return &machinepb.WaitProcessResponse{ExitCode: 0}, nil
		}
		// Try to extract exit code
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
				return &machinepb.WaitProcessResponse{ExitCode: int32(status.ExitStatus())}, nil
			}
		}
		return &machinepb.WaitProcessResponse{ExitCode: -1}, err
	}
}

// OpenSocket opens a TCP or UDP socket locally. For TCP listen=true returns a listener handle.
// For TCP listen=false returns a connected net.Conn. For UDP returns a *net.UDPConn; if listen=false
// and address/port provided, it dials; otherwise it binds to an ephemeral local port.
func (s *LocalMachineServer) OpenSocket(ctx context.Context, req *machinepb.OpenSocketRequest) (*machinepb.OpenSocketResponse, error) {
	proto := req.GetProtocol()
	addr := req.GetAddress()
	port := int(req.GetPort())
	listen := req.GetListen()

	switch proto {
	case machinepb.OpenSocketRequest_PROTO_TCP:
		if listen {
			// Force loopback binding for safety
			host := "127.0.0.1"
			l, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
			if err != nil {
				return nil, err
			}
			id := s.putHandle(l)
			return &machinepb.OpenSocketResponse{Fd: id}, nil
		}
		// client connect
		if addr == "" || port == 0 {
			return nil, fmt.Errorf("address and port required for TCP connect")
		}
		if !isLoopbackAddr(addr) {
			return nil, fmt.Errorf("TCP connect restricted to loopback")
		}
		c, err := net.Dial("tcp", net.JoinHostPort(addr, strconv.Itoa(port)))
		if err != nil {
			return nil, err
		}
		id := s.putHandle(c)
		return &machinepb.OpenSocketResponse{Fd: id}, nil

	case machinepb.OpenSocketRequest_PROTO_UDP:
		if listen {
			// Force loopback binding for safety
			udpAddr, err := net.ResolveUDPAddr("udp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
			if err != nil {
				return nil, err
			}
			uc, err := net.ListenUDP("udp", udpAddr)
			if err != nil {
				return nil, err
			}
			id := s.putHandle(uc)
			return &machinepb.OpenSocketResponse{Fd: id}, nil
		}
		// Dial if remote provided; else bind to local ephemeral
		var uc *net.UDPConn
		var err error
		if addr != "" && port != 0 {
			if !isLoopbackAddr(addr) {
				return nil, fmt.Errorf("UDP connect restricted to loopback")
			}
			uc, err = net.DialUDP("udp", nil, &net.UDPAddr{IP: net.ParseIP(addr), Port: port})
		} else {
			uc, err = net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
		}
		if err != nil {
			return nil, err
		}
		id := s.putHandle(uc)
		return &machinepb.OpenSocketResponse{Fd: id}, nil
	}

	return nil, fmt.Errorf("unsupported protocol")
}

// Accept accepts a TCP connection from a listening socket.
func (s *LocalMachineServer) Accept(ctx context.Context, req *machinepb.AcceptRequest) (*machinepb.AcceptResponse, error) {
	h, ok := s.getHandle(req.GetServerFd())
	if !ok {
		return nil, fmt.Errorf("fd %d not a listener", req.GetServerFd())
	}
	l, ok := h.(net.Listener)
	if !ok {
		return nil, fmt.Errorf("fd %d not a listener", req.GetServerFd())
	}
	c, err := l.Accept()
	if err != nil {
		return nil, err
	}
	id := s.putHandle(c)
	ra := c.RemoteAddr()
	host, portStr, _ := net.SplitHostPort(ra.String())
	p, _ := strconv.Atoi(portStr)
	return &machinepb.AcceptResponse{ClientFd: id, RemoteAddress: host, RemotePort: uint32(p)}, nil
}

// Mkdir creates a directory, optionally creating parents like `mkdir -p`.
func (s *LocalMachineServer) Mkdir(ctx context.Context, req *machinepb.MkdirRequest) (*machinepb.MkdirResponse, error) {
	if strings.TrimSpace(req.GetPath()) == "" {
		return nil, fmt.Errorf("path is required")
	}
	p := filepath.Clean(req.GetPath())
	mode := os.FileMode(req.GetMode())
	var err error
	if req.GetParents() {
		err = os.MkdirAll(p, mode)
	} else {
		err = os.Mkdir(p, mode)
	}
	return &machinepb.MkdirResponse{Success: err == nil}, err
}

// Stat returns file information; if not exists, exists=false without error.
func (s *LocalMachineServer) Stat(ctx context.Context, req *machinepb.StatRequest) (*machinepb.StatResponse, error) {
	if strings.TrimSpace(req.GetPath()) == "" {
		return nil, fmt.Errorf("path is required")
	}
	p := filepath.Clean(req.GetPath())
	var info os.FileInfo
	var err error
	if req.GetFollowSymlinks() {
		info, err = os.Stat(p)
	} else {
		info, err = os.Lstat(p)
	}
	if err != nil {
		if os.IsNotExist(err) {
			return &machinepb.StatResponse{Path: p, Exists: false}, nil
		}
		return nil, err
	}
	mt := info.ModTime()
	return &machinepb.StatResponse{
		Path:      p,
		Exists:    true,
		IsDir:     info.IsDir(),
		Size:      info.Size(),
		Mode:      uint32(info.Mode().Perm()),
		MtimeSec:  mt.Unix(),
		MtimeNsec: int64(mt.Nanosecond()),
	}, nil
}

// Chmod changes mode bits on a path.
func (s *LocalMachineServer) Chmod(ctx context.Context, req *machinepb.ChmodRequest) (*machinepb.ChmodResponse, error) {
	if strings.TrimSpace(req.GetPath()) == "" {
		return nil, fmt.Errorf("path is required")
	}
	p := filepath.Clean(req.GetPath())
	err := os.Chmod(p, os.FileMode(req.GetMode()))
	return &machinepb.ChmodResponse{Success: err == nil}, err
}

// Chown changes ownership; unsupported on Windows.
func (s *LocalMachineServer) Chown(ctx context.Context, req *machinepb.ChownRequest) (*machinepb.ChownResponse, error) {
	if strings.TrimSpace(req.GetPath()) == "" {
		return nil, fmt.Errorf("path is required")
	}
	if runtime.GOOS == "windows" {
		return nil, fmt.Errorf("chown not supported on Windows")
	}
	p := filepath.Clean(req.GetPath())
	err := os.Chown(p, int(req.GetUid()), int(req.GetGid()))
	return &machinepb.ChownResponse{Success: err == nil}, err
}

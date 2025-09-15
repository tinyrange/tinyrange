//go:build linux

package machine

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"strconv"
	"syscall"

	machinepb "github.com/tinyrange/tinyrange/vibe_party/machine/proto"
	"golang.org/x/sys/unix"
)

// execWithTTY starts the command attached to a pseudo-terminal and returns the PTY master.
func (s *LocalMachineServer) execWithTTY(ctx context.Context, cmd *exec.Cmd) (*machinepb.ExecResponse, error) {
	// Allocate PTY master
	mfd, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		// Fallback to pipe-combined
		return s.execWithTTYFallback(ctx, cmd)
	}
	// Unlock
	_ = unix.IoctlSetInt(mfd, unix.TIOCSPTLCK, 0)
	// slave number
	ptyNum, err := unix.IoctlGetInt(mfd, unix.TIOCGPTN)
	if err != nil {
		_ = unix.Close(mfd)
		return s.execWithTTYFallback(ctx, cmd)
	}
	slavePath := "/dev/pts/" + strconv.Itoa(ptyNum)
	sfd, err := unix.Open(slavePath, unix.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		_ = unix.Close(mfd)
		return s.execWithTTYFallback(ctx, cmd)
	}

	masterFile := os.NewFile(uintptr(mfd), "ptmx")
	slaveFile := os.NewFile(uintptr(sfd), slavePath)

	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: int(slaveFile.Fd())}
	cmd.Stdin = slaveFile
	cmd.Stdout = slaveFile
	cmd.Stderr = slaveFile

	if err := cmd.Start(); err != nil {
		_ = masterFile.Close()
		_ = slaveFile.Close()
		return nil, err
	}
	// Parent closes slave; child holds dup
	_ = slaveFile.Close()

	id := s.putHandle(masterFile)

	pid := int32(cmd.Process.Pid)
	s.mu.Lock()
	s.procs[pid] = cmd
	s.mu.Unlock()

	return &machinepb.ExecResponse{
		Pid:      pid,
		StdinFd:  id,
		StdoutFd: id,
		StderrFd: 0,
	}, nil
}

// execWithTTYFallback uses a single pipe to combine output; not a true PTY.
func (s *LocalMachineServer) execWithTTYFallback(ctx context.Context, cmd *exec.Cmd) (*machinepb.ExecResponse, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	cmd.Stdout = w
	cmd.Stderr = w
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	_ = w.Close()
	sout := bufio.NewReader(r)

	stdinID := s.putHandle(in)
	stdoutID := s.putHandle(sout)

	pid := int32(cmd.Process.Pid)
	s.mu.Lock()
	s.procs[pid] = cmd
	s.mu.Unlock()
	return &machinepb.ExecResponse{Pid: pid, StdinFd: stdinID, StdoutFd: stdoutID, StderrFd: 0}, nil
}

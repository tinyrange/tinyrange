//go:build !linux

package machine

import (
    "bufio"
    "context"
    "os"
    "os/exec"

    machinepb "github.com/tinyrange/tinyrange/vibe_party/machine/proto"
)

// execWithTTY provides a portable fallback that combines stdout/stderr via a pipe.
func (s *LocalMachineServer) execWithTTY(ctx context.Context, cmd *exec.Cmd) (*machinepb.ExecResponse, error) {
    // Combined output using a single os.Pipe; not a true PTY
    r, w, err := os.Pipe()
    if err != nil { return nil, err }
    cmd.Stdout = w
    cmd.Stderr = w
    in, err := cmd.StdinPipe()
    if err != nil { return nil, err }
    if err := cmd.Start(); err != nil { return nil, err }
    _ = w.Close()
    sout := bufio.NewReader(r)

    stdinID := s.putHandle(in)
    stdoutID := s.putHandle(sout)

    pid := int32(cmd.Process.Pid)
    s.mu.Lock(); s.procs[pid] = cmd; s.mu.Unlock()
    return &machinepb.ExecResponse{Pid: pid, StdinFd: stdinID, StdoutFd: stdoutID, StderrFd: 0}, nil
}


//go:build linux

package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/anmitsu/go-shlex"
	"github.com/creack/pty"
	"github.com/insomniacslk/dhcp/netboot"
	"github.com/jsimonetti/rtnetlink/rtnl"
	"github.com/ramr/go-reaper"
	"github.com/schollz/progressbar/v3"
	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/config"
	starlarkjson "go.starlark.net/lib/json"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
	"golang.org/x/crypto/ssh"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

var START_TIME = time.Now()

var starlarkJsonDecode = starlarkjson.Module.Members["decode"].(*starlark.Builtin).CallInternal

func GetUptime() (time.Duration, error) {
	var ts unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_MONOTONIC, &ts); err != nil {
		return 0, err
	}

	return time.Duration(ts.Nano()), nil
}

func ToStringList(it starlark.Iterable) ([]string, error) {
	iter := it.Iterate()
	defer iter.Done()

	var ret []string

	var val starlark.Value
	for iter.Next(&val) {
		str, ok := starlark.AsString(val)
		if !ok {
			return nil, fmt.Errorf("could not convert %s to string", val.Type())
		}

		ret = append(ret, str)
	}

	return ret, nil
}

// parseDims extracts terminal dimensions (width x height) from the provided buffer.
func parseDims(b []byte) (uint32, uint32) {
	w := binary.BigEndian.Uint32(b)
	h := binary.BigEndian.Uint32(b[4:])
	return w, h
}

// Winsize stores the Height and Width of a terminal.
type Winsize struct {
	Height uint16
	Width  uint16
	x      uint16 // unused
	y      uint16 // unused
}

// SetWinsize sets the size of the given pty.
func SetWinsize(fd uintptr, w, h uint32) error {
	ws := &Winsize{Width: uint16(w), Height: uint16(h)}
	_, _, err := syscall.Syscall(syscall.SYS_IOCTL, fd, uintptr(syscall.TIOCSWINSZ), uintptr(unsafe.Pointer(ws)))
	return err
}

type sshServer struct {
	callable starlark.Callable
	command  []string
	hostKey  string
	password string
}

// Attr implements starlark.HasAttrs.
func (s *sshServer) Attr(name string) (starlark.Value, error) {
	if name == "run" {
		return starlark.NewBuiltin("SSHServer.run", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				cmdArgs starlark.Iterable
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"args", &cmdArgs,
			); err != nil {
				return starlark.None, err
			}

			var err error

			s.command, err = ToStringList(cmdArgs)
			if err != nil {
				return starlark.None, err
			}

			return starlark.None, nil
		}), nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (s *sshServer) AttrNames() []string {
	return []string{"run"}
}

func (s *sshServer) attachShell(conn ssh.Conn, connection ssh.Channel, nonInteractive bool, env []string, resizes <-chan []byte) error {
	if s.callable != nil {
		if _, err := starlark.Call(&starlark.Thread{}, s.callable, starlark.Tuple{s}, []starlark.Tuple{}); err != nil {
			return err
		}
	}

	shell := exec.Command(s.command[0], s.command[1:]...)

	shell.Env = env

	// Only redirect stderr if we're not in interactive mode
	// TODO(joshua): Don't allocate a pty if we're not in interactive mode
	if nonInteractive {
		shell.Stderr = connection.Stderr()
	}

	close := func() {
		if shell.Process != nil {
			if ps, err := shell.Process.Wait(); err != nil && ps != nil {
				slog.Warn("failed to exit shell", "error", err)
			}
		}

		connection.Close()
	}

	//start a shell for this channel's connection
	shellf, err := pty.Start(shell)
	if err != nil {
		close()
		return fmt.Errorf("could not start pty: %s", err)
	}

	//dequeue resizes
	go func() {
		for payload := range resizes {
			w, h := parseDims(payload)
			_ = SetWinsize(shellf.Fd(), w, h)
		}
	}()

	//pipe session to shell and visa-versa
	go func() {
		err := common.Proxy(shellf, connection, 4096)
		if err != nil {
			slog.Warn("proxy failed", "error", err)
		}

		close()
	}()

	go func() {
		// Start proactively listening for process death, for those ptys that
		// don't signal on EOF.
		if shell.Process != nil {
			if ps, err := shell.Process.Wait(); err != nil && ps != nil {
				slog.Warn("failed to exit shell", "error", err)
			}

			// It appears that closing the pty is an idempotent operation
			// therefore making this call ensures that the other two coroutines
			// will fall through and exit, and there is no downside.

			// Well it does have a downside. Closing immediately will prevent
			// the remaining IO from flushing.
			// This is currently a bad hack and I should do something more
			// intelligent here.
			time.Sleep(50 * time.Millisecond)

			shellf.Close()
		}
	}()
	return nil
}

func (s *sshServer) handleChannel(conn ssh.Conn, newChannel ssh.NewChannel) {
	if t := newChannel.ChannelType(); t != "session" {
		_ = newChannel.Reject(ssh.UnknownChannelType, fmt.Sprintf("unknown channel type: %s", t))
		return
	}

	connection, requests, err := newChannel.Accept()
	if err != nil {
		slog.Warn("could not accept channel", "error", err)
		return
	}

	go s.handleRequests(conn, connection, requests)
}

func (s *sshServer) handleExec(conn ssh.Conn, ch ssh.Channel, req *ssh.Request, env []string) error {
	// Parse the command
	command := string(req.Payload[4:])

	args, err := shlex.Split(command, true)
	if err != nil {
		return fmt.Errorf("failed to parse command: %s", err)
	}

	cmd := exec.Command(args[0], args[1:]...)

	cmd.Env = env

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	input, err := cmd.StdinPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to run command: %s", err)
	}

	req.Reply(true, nil)
	go io.Copy(input, ch)
	io.Copy(ch, stdout)
	io.Copy(ch.Stderr(), stderr)

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("failed to wait for command: %s", err)
	}

	ch.SendRequest("exit-status", false, []byte{0, 0, 0, 0})

	return nil
}

func (s *sshServer) handleRequests(conn ssh.Conn, connection ssh.Channel, requests <-chan *ssh.Request) {
	// prepare to handle client requests
	env := os.Environ()

	resizes := make(chan []byte, 10)

	defer close(resizes)

	var nonInteractive bool

	// Sessions have out-of-band requests such as "shell", "pty-req" and "env"
	for req := range requests {
		switch req.Type {
		case "pty-req":
			slog.Debug("pty-req", "payload", hex.EncodeToString(req.Payload))
			termLen := req.Payload[3]

			// Make sure we correctly forward the terminal from the host.
			term := string(req.Payload[4 : 4+termLen])
			env = append(env, fmt.Sprintf("TERM=%s", term))

			if strings.HasPrefix(term, "non-interactive/") {
				nonInteractive = true
			}

			resizes <- req.Payload[termLen+4:]
			// Responding true (OK) here will let the client
			// know we have a pty ready
			_ = req.Reply(true, nil)
		case "window-change":
			resizes <- req.Payload
		case "shell":
			// Responding true (OK) here will let the client
			// know we have attached the shell (pty) to the connection
			if len(req.Payload) > 0 {
				slog.Debug("shell command ignored", "payload", req.Payload)
			}

			err := s.attachShell(conn, connection, nonInteractive, env, resizes)
			if err != nil {
				slog.Warn("failed to attach shell", "error", err)
			}

			_ = req.Reply(err == nil, nil)
		case "exec":
			err := s.handleExec(conn, connection, req, env)
			if err != nil {
				slog.Warn("failed to handle exec", "error", err)
			}

			if err := connection.Close(); err != nil {
				slog.Warn("failed to close connection", "error", err)
			}
		default:
			slog.Debug("unknown request", "type", req.Type, "reply", req.WantReply, "data", req.Payload)

			if req.WantReply {
				req.Reply(false, nil)
			}
		}
	}
}

func (s *sshServer) handleChannels(conn ssh.Conn, chans <-chan ssh.NewChannel) {
	// Service the incoming Channel channel in go routine
	for newChannel := range chans {
		go s.handleChannel(conn, newChannel)
	}
}

func (s *sshServer) handleClient(nConn net.Conn, config *ssh.ServerConfig) error {
	// Before use, a handshake must be performed on the incoming net.Conn.
	sshConn, chans, reqs, err := ssh.NewServerConn(nConn, config)
	if err != nil {
		return err
	}

	slog.Debug("new SSH connection", "remote", sshConn.RemoteAddr(), "client_version", sshConn.ClientVersion())

	// Discard all global out-of-band Requests
	go ssh.DiscardRequests(reqs)

	// Accept all channels
	go s.handleChannels(sshConn, chans)

	return nil
}

func (s *sshServer) run(callable starlark.Callable) error {
	s.callable = callable

	listener, err := net.Listen("tcp", "0.0.0.0:2222")
	if err != nil {
		return fmt.Errorf("ssh: failed to listen for connection: %v", err)
	}

	config := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if subtle.ConstantTimeCompare(pass, []byte(s.password)) == 1 {
				return nil, nil
			}
			return nil, fmt.Errorf("password rejected for %q", c.User())
		},
	}

	if s.hostKey == "" {
		privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return fmt.Errorf("ssh: failed to generate key: %v", err)
		}

		hostSigner, err := ssh.NewSignerFromKey(privateKey)
		if err != nil {
			return fmt.Errorf("ssh: failed to make signer: %v", err)
		}

		config.AddHostKey(hostSigner)
	} else {
		private, err := ssh.ParsePrivateKey([]byte(s.hostKey))
		if err != nil {
			return fmt.Errorf("ssh: failed to parse private key: %v", err)
		}

		config.AddHostKey(private)
	}

	for {
		nConn, err := listener.Accept()
		if err != nil {
			return err
		}
		go func() {
			err := s.handleClient(nConn, config)
			if err != nil {
				slog.Debug("failed to handle ssh client", "err", err)
			}
		}()
	}
}

func (*sshServer) String() string        { return "SSHServer" }
func (*sshServer) Type() string          { return "SSHServer" }
func (*sshServer) Hash() (uint32, error) { return 0, fmt.Errorf("SSHServer is not hashable") }
func (*sshServer) Truth() starlark.Bool  { return starlark.True }
func (*sshServer) Freeze()               {}

var (
	_ starlark.Value    = &sshServer{}
	_ starlark.HasAttrs = &sshServer{}
)

type mountOptions struct {
	Readonly bool
	Ensure   bool
}

func mount(kind string, mountName string, mountPoint string, opts mountOptions) error {
	var flags uintptr
	if opts.Readonly {
		flags |= unix.MS_RDONLY
	}
	if opts.Ensure {
		if err := common.Ensure(mountPoint, os.ModePerm); err != nil {
			return fmt.Errorf("failed to create mount point: %v", err)
		}
	}
	err := unix.Mount(mountName, mountPoint, kind, flags, "")
	if err != nil {
		return fmt.Errorf("failed mounting %s(%s) on %s: %v", mountName, kind, mountPoint, err)
	}
	return nil
}

// FdReader is an io.Reader with an Fd function
type FdReader interface {
	io.Reader
	Fd() uintptr
}

func getFd(reader io.Reader) (fd int, ok bool) {
	fdthing, ok := reader.(FdReader)
	if !ok {
		return 0, false
	}

	fd = int(fdthing.Fd())
	return fd, term.IsTerminal(fd)
}

func loadStarlarkArgs() (starlark.Value, error) {
	var args starlark.Value = starlark.NewDict(0)

	if ok, _ := common.Exists("/init.json"); ok {
		contents, err := os.ReadFile("/init.json")
		if err != nil {
			return nil, err
		}

		args, err = starlarkJsonDecode(nil, starlark.Tuple{starlark.String(contents)}, []starlark.Tuple{})
		if err != nil {
			return nil, err
		}
	}

	if ok, _ := common.Exists("/init.d"); ok {
		if args == nil {
			args = starlark.NewDict(0)
		}

		argsDict, ok := args.(*starlark.Dict)
		if ok {
			files, err := os.ReadDir("/init.d")
			if err != nil {
				return nil, err
			}

			for _, file := range files {
				if file.IsDir() {
					continue
				}

				if strings.HasSuffix(file.Name(), ".json") {
					contents, err := os.ReadFile("/init.d/" + file.Name())
					if err != nil {
						return nil, err
					}

					var newArgs map[string]string

					if err := json.Unmarshal(contents, &newArgs); err != nil {
						return nil, err
					}

					for k, v := range newArgs {
						if err := argsDict.SetKey(starlark.String(k), starlark.String(v)); err != nil {
							return nil, err
						}
					}
				}
			}
		}
	}

	return args, nil
}

func runStarlark(filename string) error {
	args, err := loadStarlarkArgs()
	if err != nil {
		return err
	}

	globals := starlark.StringDict{}

	globals["args"] = args

	globals["exit"] = starlark.NewBuiltin("exit", func(
		thread *starlark.Thread,
		fn *starlark.Builtin,
		args starlark.Tuple,
		kwargs []starlark.Tuple,
	) (starlark.Value, error) {
		os.Exit(0)

		return starlark.None, nil
	})

	globals["network_interface_up"] = starlark.NewBuiltin("network_interface_up", func(
		thread *starlark.Thread,
		fn *starlark.Builtin,
		args starlark.Tuple,
		kwargs []starlark.Tuple,
	) (starlark.Value, error) {
		var (
			ifname string
		)

		if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
			"ifname", &ifname,
		); err != nil {
			return starlark.None, err
		}

		rt, err := rtnl.Dial(nil)
		if err != nil {
			return starlark.None, fmt.Errorf("failed to dial netlink: %v", err)
		}
		defer rt.Close()

		ifc, err := net.InterfaceByName(ifname)
		if err != nil {
			return starlark.None, fmt.Errorf("failed to get interface: %v", err)
		}

		err = rt.LinkUp(ifc)
		if err != nil {
			return starlark.None, fmt.Errorf("failed to bring link up: %v", err)
		}

		return starlark.None, nil
	})

	globals["network_interface_configure"] = starlark.NewBuiltin("network_interface_configure", func(
		thread *starlark.Thread,
		fn *starlark.Builtin,
		args starlark.Tuple,
		kwargs []starlark.Tuple,
	) (starlark.Value, error) {
		var (
			name   string
			ip     string
			router string
		)

		if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
			"name", &name,
			"ip", &ip,
			"router", &router,
		); err != nil {
			return starlark.None, err
		}

		ipAddr, cidr, err := net.ParseCIDR(ip)
		if err != nil {
			return starlark.None, err
		}

		cidr.IP = ipAddr

		if err := netboot.ConfigureInterface(name, &netboot.NetConf{
			Addresses: []netboot.AddrConf{
				{IPNet: *cidr},
			},
			DNSServers: []net.IP{net.ParseIP(router)},
			Routers:    []net.IP{net.ParseIP(router)},
		}); err != nil {
			return nil, fmt.Errorf("failed to configure interface: %v", err)
		}

		slog.Debug("configured networking statically", "routers", router)

		return starlark.String(router), nil
	})

	globals["fetch_http"] = starlark.NewBuiltin("fetch_http", func(
		thread *starlark.Thread,
		fn *starlark.Builtin,
		args starlark.Tuple,
		kwargs []starlark.Tuple,
	) (starlark.Value, error) {
		var (
			urlString string
		)

		if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
			"url", &urlString,
		); err != nil {
			return starlark.None, err
		}

		resp, err := http.Get(urlString)
		if err != nil {
			return starlark.None, err
		}
		defer resp.Body.Close()

		contents, err := io.ReadAll(resp.Body)
		if err != nil {
			return starlark.None, err
		}

		return starlark.String(contents), nil
	})

	globals["run"] = starlark.NewBuiltin("run", func(
		thread *starlark.Thread,
		fn *starlark.Builtin,
		args starlark.Tuple,
		kwargs []starlark.Tuple,
	) (starlark.Value, error) {
		var cmdArgs []string

		for _, arg := range args {
			str, ok := starlark.AsString(arg)
			if !ok {
				return starlark.None, fmt.Errorf("expected string got %s", arg.Type())
			}

			cmdArgs = append(cmdArgs, str)
		}

		cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)

		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin

		if err := cmd.Run(); err != nil {
			return starlark.None, err
		}

		return starlark.None, nil
	})

	globals["set_hostname"] = starlark.NewBuiltin("set_hostname", func(
		thread *starlark.Thread,
		fn *starlark.Builtin,
		args starlark.Tuple,
		kwargs []starlark.Tuple,
	) (starlark.Value, error) {
		var (
			hostname string
		)

		if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
			"hostname", &hostname,
		); err != nil {
			return starlark.None, err
		}

		if err := unix.Sethostname([]byte(hostname)); err != nil {
			return starlark.None, err
		}

		return starlark.None, nil
	})

	globals["mount"] = starlark.NewBuiltin("linux_mount", func(
		thread *starlark.Thread,
		fn *starlark.Builtin,
		args starlark.Tuple,
		kwargs []starlark.Tuple,
	) (starlark.Value, error) {
		var (
			fsKind      string
			name        string
			mountPoint  string
			ensurePath  bool
			ignoreError bool
		)

		if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
			"kind", &fsKind,
			"name", &name,
			"mount_point", &mountPoint,
			"ensure_path?", &ensurePath,
			"ignore_error?", &ignoreError,
		); err != nil {
			return starlark.None, err
		}

		if ensurePath {
			err := common.Ensure(mountPoint, os.ModePerm)

			if err != nil && !ignoreError {
				return starlark.None, fmt.Errorf("failed to create mount point: %v", err)
			}
		}

		err := mount(fsKind, name, mountPoint, mountOptions{})
		if err != nil && !ignoreError {
			return starlark.None, fmt.Errorf("failed to mount: %v", err)
		}

		return starlark.None, nil
	})

	globals["path_ensure"] = starlark.NewBuiltin("path_ensure", func(
		thread *starlark.Thread,
		fn *starlark.Builtin,
		args starlark.Tuple,
		kwargs []starlark.Tuple,
	) (starlark.Value, error) {
		var (
			path string
		)

		if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
			"path", &path,
		); err != nil {
			return starlark.None, err
		}

		if err := common.Ensure(path, os.ModePerm); err != nil {
			return starlark.None, err
		}

		return starlark.None, nil
	})

	globals["path_symlink"] = starlark.NewBuiltin("path_symlink", func(
		thread *starlark.Thread,
		fn *starlark.Builtin,
		args starlark.Tuple,
		kwargs []starlark.Tuple,
	) (starlark.Value, error) {
		var (
			source string
			target string
		)

		if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
			"source", &source,
			"target", &target,
		); err != nil {
			return starlark.None, err
		}

		if err := os.Symlink(source, target); err != nil {
			return starlark.None, err
		}

		return starlark.None, nil
	})

	globals["file_read"] = starlark.NewBuiltin("file_read", func(
		thread *starlark.Thread,
		fn *starlark.Builtin,
		args starlark.Tuple,
		kwargs []starlark.Tuple,
	) (starlark.Value, error) {
		var (
			path string
		)

		if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
			"path", &path,
		); err != nil {
			return starlark.None, err
		}

		contents, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}

		return starlark.String(contents), nil
	})

	globals["file_write"] = starlark.NewBuiltin("file_write", func(
		thread *starlark.Thread,
		fn *starlark.Builtin,
		args starlark.Tuple,
		kwargs []starlark.Tuple,
	) (starlark.Value, error) {
		var (
			path     string
			contents string
		)

		if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
			"path", &path,
			"contents", &contents,
		); err != nil {
			return starlark.None, err
		}

		if err := os.WriteFile(path, []byte(contents), os.ModePerm); err != nil {
			return starlark.None, err
		}

		return starlark.None, nil
	})

	globals["file_chmod"] = starlark.NewBuiltin("file_chmod", func(
		thread *starlark.Thread,
		fn *starlark.Builtin,
		args starlark.Tuple,
		kwargs []starlark.Tuple,
	) (starlark.Value, error) {
		var (
			path string
			mode int
		)

		if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
			"path", &path,
			"mode", &mode,
		); err != nil {
			return starlark.None, err
		}

		if err := os.Chmod(path, os.FileMode(mode)); err != nil {
			return starlark.None, err
		}

		return starlark.None, nil
	})

	globals["insmod"] = starlark.NewBuiltin("insmod", func(
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

		if err := unix.InitModule([]byte(contents), ""); err != nil {
			return starlark.None, err
		}

		return starlark.None, nil
	})

	globals["chroot"] = starlark.NewBuiltin("chroot", func(
		thread *starlark.Thread,
		fn *starlark.Builtin,
		args starlark.Tuple,
		kwargs []starlark.Tuple,
	) (starlark.Value, error) {
		var (
			filename string
		)

		if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
			"filename", &filename,
		); err != nil {
			return starlark.None, err
		}

		if err := unix.Chroot(filename); err != nil {
			return starlark.None, err
		}

		return starlark.None, nil
	})

	globals["chdir"] = starlark.NewBuiltin("chdir", func(
		thread *starlark.Thread,
		fn *starlark.Builtin,
		args starlark.Tuple,
		kwargs []starlark.Tuple,
	) (starlark.Value, error) {
		var (
			filename string
		)

		if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
			"filename", &filename,
		); err != nil {
			return starlark.None, err
		}

		if err := unix.Chdir(filename); err != nil {
			return starlark.None, err
		}

		return starlark.None, nil
	})

	globals["exec"] = starlark.NewBuiltin("exec", func(
		thread *starlark.Thread,
		fn *starlark.Builtin,
		args starlark.Tuple,
		kwargs []starlark.Tuple,
	) (starlark.Value, error) {
		cmdArgs, err := ToStringList(args)
		if err != nil {
			return nil, err
		}

		slog.Debug("exec", "args", cmdArgs)

		if err := unix.Exec(cmdArgs[0], cmdArgs, os.Environ()); err != nil {
			return starlark.None, err
		}

		return starlark.None, nil
	})

	globals["run_ssh_server"] = starlark.NewBuiltin("run_ssh_server", func(
		thread *starlark.Thread,
		fn *starlark.Builtin,
		args starlark.Tuple,
		kwargs []starlark.Tuple,
	) (starlark.Value, error) {
		var (
			callable starlark.Callable
			password string
			hostKey  string
		)

		if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
			"callable", &callable,
			"password?", &password,
			"host_key?", &hostKey,
		); err != nil {
			return starlark.None, err
		}

		if password == "" {
			password = config.INSECURE_SSH_PASSWORD
		}

		sshServer := &sshServer{
			hostKey:  hostKey,
			password: password,
		}

		err := sshServer.run(callable)
		if err != nil {
			return starlark.None, err
		}

		return starlark.None, nil
	})

	globals["parse_commandline"] = starlark.NewBuiltin("parse_commandline", func(
		thread *starlark.Thread,
		fn *starlark.Builtin,
		args starlark.Tuple,
		kwargs []starlark.Tuple,
	) (starlark.Value, error) {
		var (
			cmdline string
		)

		if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
			"cmdline", &cmdline,
		); err != nil {
			return starlark.None, err
		}

		cmdline = strings.TrimSuffix(cmdline, "\n")

		for _, arg := range strings.Split(cmdline, " ") {
			if arg == "tinyrange.verbose=on" {
				if err := common.EnableVerbose(); err != nil {
					return starlark.None, err
				}
			} else if arg == "tinyrange.nonet=yes" {
				if err := os.Setenv("TINYRANGE_NONET", "yes"); err != nil {
					return starlark.None, err
				}
			} else if strings.HasPrefix(arg, "tinyrange.experimental=") {
				flags := strings.TrimPrefix(arg, "tinyrange.experimental=")

				if err := common.SetExperimental(strings.Split(flags, ",")); err != nil {
					return starlark.None, err
				}
			} else if strings.HasPrefix(arg, "tinyrange.interaction=") {
				interaction := strings.TrimPrefix(arg, "tinyrange.interaction=")

				if err := os.Setenv("TINYRANGE_INTERACTION", interaction); err != nil {
					return starlark.None, err
				}
			}
		}

		return starlark.None, nil
	})

	globals["set_env"] = starlark.NewBuiltin("set_env", func(
		thread *starlark.Thread,
		fn *starlark.Builtin,
		args starlark.Tuple,
		kwargs []starlark.Tuple,
	) (starlark.Value, error) {
		var (
			key   string
			value string
		)

		if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
			"key", &key,
			"value", &value,
		); err != nil {
			return starlark.None, err
		}

		if err := os.Setenv(key, value); err != nil {
			return starlark.None, err
		}

		return starlark.None, nil
	})

	globals["get_env"] = starlark.NewBuiltin("get_env", func(
		thread *starlark.Thread,
		fn *starlark.Builtin,
		args starlark.Tuple,
		kwargs []starlark.Tuple,
	) (starlark.Value, error) {
		var (
			key string
		)

		if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
			"key", &key,
		); err != nil {
			return starlark.None, err
		}

		return starlark.String(os.Getenv(key)), nil
	})

	globals["json"] = starlarkjson.Module

	var uname unix.Utsname

	if err := unix.Uname(&uname); err != nil {
		return err
	}

	unameDict := starlark.NewDict(8)

	unameDict.SetKey(starlark.String("domainname"), starlark.String(
		bytes.Trim(uname.Domainname[:], "\x00"),
	))
	unameDict.SetKey(starlark.String("machine"), starlark.String(
		bytes.Trim(uname.Machine[:], "\x00"),
	))
	unameDict.SetKey(starlark.String("nodename"), starlark.String(
		bytes.Trim(uname.Nodename[:], "\x00"),
	))
	unameDict.SetKey(starlark.String("release"), starlark.String(
		bytes.Trim(uname.Release[:], "\x00"),
	))
	unameDict.SetKey(starlark.String("sysname"), starlark.String(
		bytes.Trim(uname.Sysname[:], "\x00"),
	))
	unameDict.SetKey(starlark.String("version"), starlark.String(
		bytes.Trim(uname.Version[:], "\x00"),
	))

	globals["uname"] = unameDict

	thread := &starlark.Thread{Name: "init"}

	decls, err := starlark.ExecFileOptions(&syntax.FileOptions{Set: true, While: true, TopLevelControl: true}, thread, filename, nil, globals)
	if err != nil {
		return err
	}

	mainFunc, ok := decls["main"]
	if !ok {
		return fmt.Errorf("expected Callable got %s", mainFunc.Type())
	}

	_, err = starlark.Call(thread, mainFunc, starlark.Tuple{}, []starlark.Tuple{})
	if err != nil {
		return err
	}

	return nil
}

func runSSHServer() error {
	// Load the configuration file.
	args, err := loadStarlarkArgs()
	if err != nil {
		return err
	}

	argsDict, ok := args.(*starlark.Dict)
	if !ok {
		return fmt.Errorf("args is not a dict")
	}

	sshCommand, found, err := argsDict.Get(starlark.String("ssh_command"))
	if err != nil {
		return fmt.Errorf("failed to get ssh_command: %s", err)
	}

	var commandArgs []string

	if iter, ok := sshCommand.(starlark.Iterable); ok {
		commandArgs, err = ToStringList(iter)
		if err != nil {
			return fmt.Errorf("failed to convert ssh_command to list: %s", err)
		}
	} else {
		return fmt.Errorf("ssh_command is not iterable")
	}

	if !found {
		return fmt.Errorf("ssh_command not found")
	}

	sshHostKey, err := getString(argsDict, "ssh_host_key")
	if err != nil {
		return fmt.Errorf("failed to get ssh_host_key: %s", err)
	}

	sshPassword, err := getString(argsDict, "ssh_password")
	if err != nil {
		return fmt.Errorf("failed to get ssh_password: %s", err)
	}

	server := &sshServer{
		hostKey:  sshHostKey,
		password: sshPassword,
		command:  commandArgs,
	}

	slog.Info("starting ssh server")

	if err := server.run(nil); err != nil {
		return fmt.Errorf("failed to start ssh server: %s", err)
	}

	return nil
}

var (
	execShell         = flag.Bool("shell", false, "start the shell instead of running /init.sh")
	runSshServer      = flag.String("ssh", "", "run a ssh server that executes the argument on connection")
	runConfiguredSsh  = flag.Bool("ssh-configured", false, "run a ssh server with the machine config files")
	downloadFile      = flag.String("download", "", "download a file from the specified server")
	runScripts        = flag.String("run-scripts", "", "run a JSON file of scripts")
	lockFile          = flag.String("lock-file", "", "don't run scripts if this file exists and create it if it doesn't exist")
	runBasicScripts   = flag.String("run-basic-scripts", "", "run a JSON file containing an array of commands")
	translateScripts  = flag.Bool("translate-scripts", false, "translate scripts into starlark before running them")
	runConfig         = flag.String("run-config", "", "run a JSON file with a given builder config")
	dumpFs            = flag.String("dump-fs", "", "dump all filesystem metadata to a CSV file")
	runStarlarkScript = flag.String("star", "", "run a starlark script")
	modprobe          = flag.String("modprobe", "", "load a kernel module")
)

func initMain() error {
	flag.Parse()

	if *execShell {
		return shellMain()
	}

	if *runSshServer != "" {
		cmd, err := shlex.Split(*runSshServer, true)
		if err != nil {
			return err
		}

		sshServer := &sshServer{command: cmd, password: config.INSECURE_SSH_PASSWORD}

		return sshServer.run(nil)
	}

	if *runConfiguredSsh {
		return runSSHServer()
	}

	if *downloadFile != "" {
		resp, err := http.Get(*downloadFile)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		pb := progressbar.DefaultBytes(resp.ContentLength)

		out, err := os.Create("out.bin")
		if err != nil {
			return err
		}

		if _, err := io.Copy(io.MultiWriter(pb, out), resp.Body); err != nil {
			return err
		}
	}

	if *dumpFs != "" {
		return common.DumpFs(*dumpFs)
	}

	if *runScripts != "" {
		if *lockFile != "" {
			if _, err := os.Stat(*lockFile + ".tmp"); err == nil {
				for {
					if _, err := os.Stat(*lockFile); err == nil {
						slog.Info("waiting for lock file to be removed", "filename", *lockFile+".tmp")
						time.Sleep(100 * time.Millisecond)
						continue
					} else {
						break
					}
				}

				return nil
			}

			if _, err := os.Stat(*lockFile); err == nil {
				return nil
			}

			if err := os.WriteFile(*lockFile+".tmp", []byte{}, os.ModePerm); err != nil {
				return err
			}
			defer os.Remove(*lockFile + ".tmp")

			if err := os.WriteFile(*lockFile, []byte{}, os.ModePerm); err != nil {
				return err
			}
		}

		if common.HasExperimentalFlag("translate_shell") {
			*translateScripts = true
		}
		return builderRunScripts(*runScripts, *translateScripts)
	}

	if *runBasicScripts != "" {
		bytes, err := os.ReadFile(*runBasicScripts)
		if err != nil {
			return err
		}

		var scripts []string

		if err := json.Unmarshal(bytes, &scripts); err != nil {
			return err
		}

		for _, script := range scripts {
			if err := common.RunCommand(script); err != nil {
				return err
			}
		}

		return nil
	}

	if *runConfig != "" {
		f, err := os.Open(*runConfig)
		if err != nil {
			return err
		}

		dec := json.NewDecoder(f)

		var cfg config.BuilderConfig

		if err := dec.Decode(&cfg); err != nil {
			return err
		}

		return builderRunWithConfig(cfg)
	}

	if *runStarlarkScript != "" {
		return runStarlark(*runStarlarkScript)
	}

	if *modprobe != "" {
		return common.Modprobe(*modprobe)
	}

	if os.Getuid() != 0 {
		return fmt.Errorf("/init must be run as root")
	}

	needsReaper := false

	// get the interaction from /proc/cmdline
	if err := mount("proc", "proc", "/proc", mountOptions{
		Ensure: true,
	}); err != nil {
		return err
	}

	cmdline, err := os.ReadFile("/proc/cmdline")
	if err != nil {
		return err
	}

	for _, arg := range strings.Split(string(cmdline), " ") {
		if strings.HasPrefix(arg, "tinyrange.interaction=") {
			interaction := strings.TrimPrefix(arg, "tinyrange.interaction=")

			interaction = strings.Trim(interaction, "\n")

			if interaction != "serial" {
				needsReaper = true
			}
		}
	}

	// Use an environment variable REAPER to indicate whether or not
	// we are the child/parent.
	if _, hasReaper := os.LookupEnv("REAPER"); !hasReaper && needsReaper {
		if os.Getpid() != 1 {
			return fmt.Errorf("/init must run as PID 1")
		}

		//  Start background reaping of orphaned child processes.
		go reaper.Reap()

		args := os.Args

		pwd, err := os.Getwd()
		if err != nil {
			panic(err)
		}

		kidEnv := []string{fmt.Sprintf("REAPER=%d", os.Getpid())}

		var wstatus syscall.WaitStatus
		pattrs := &syscall.ProcAttr{
			Dir: pwd,
			Env: append(os.Environ(), kidEnv...),
			Sys: &syscall.SysProcAttr{Setsid: true},
			Files: []uintptr{
				uintptr(syscall.Stdin),
				uintptr(syscall.Stdout),
				uintptr(syscall.Stderr),
			},
		}

		pid, _ := syscall.ForkExec(args[0], args, pattrs)

		_, err = syscall.Wait4(pid, &wstatus, 0, nil)
		for syscall.EINTR == err {
			_, err = syscall.Wait4(pid, &wstatus, 0, nil)
		}

		// If you put this code into a function, then exit here.
		os.Exit(0)
		return nil
	}

	if err := os.Setenv("TINYRANGE_START_TIME", fmt.Sprintf("%d", START_TIME.UnixMicro())); err != nil {
		return err
	}

	if err := runStarlark("/init.star"); err != nil {
		return err
	}

	return nil
}

func main() {
	if os.Getenv("TINYRANGE_VERBOSE") == "on" {
		if err := common.EnableVerbose(); err != nil {
			slog.Error("failed to enable verbose logging", "err", err)
			os.Exit(1)
		}
	}

	if err := initMain(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

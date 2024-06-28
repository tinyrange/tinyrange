package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/creack/pty"
	"github.com/insomniacslk/dhcp/netboot"
	"github.com/jsimonetti/rtnetlink/rtnl"
	"github.com/tinyrange/tinyrange/pkg/common"
	starlarkjson "go.starlark.net/lib/json"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
	"golang.org/x/crypto/ssh"
	"golang.org/x/sys/unix"
)

var starlarkJsonDecode = starlarkjson.Module.Members["decode"].(*starlark.Builtin).CallInternal

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

type sshServer struct {
	callable starlark.Callable
	command  []string
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

func (s *sshServer) attachShell(conn ssh.Conn, connection ssh.Channel, env []string, resizes <-chan []byte) error {
	if _, err := starlark.Call(&starlark.Thread{}, s.callable, starlark.Tuple{s}, []starlark.Tuple{}); err != nil {
		return err
	}

	shell := exec.Command(s.command[0], s.command[1:]...)

	shell.Env = env

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
		return fmt.Errorf("could not start pty (%s)", err)
	}

	//dequeue resizes
	// go func() {
	// 	for payload := range resizes {
	// 		w, h := parseDims(payload)
	// 		_ = SetWinsize(shellf.Fd(), w, h)
	// 	}
	// }()

	//pipe session to shell and visa-versa
	go func() {
		err := common.Proxy(connection, shellf)
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

func (s *sshServer) handleRequests(conn ssh.Conn, connection ssh.Channel, requests <-chan *ssh.Request) {
	// prepare to handle client requests
	env := os.Environ()

	resizes := make(chan []byte, 10)

	defer close(resizes)

	// Sessions have out-of-band requests such as "shell", "pty-req" and "env"
	for req := range requests {
		switch req.Type {
		case "pty-req":
			slog.Info("pty-req", "payload", hex.EncodeToString(req.Payload))
			termLen := req.Payload[3]

			// Make sure we correctly forward the terminal from the host.
			term := string(req.Payload[4 : 4+termLen])
			env = append(env, fmt.Sprintf("TERM=%s", term))

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

			err := s.attachShell(conn, connection, env, resizes)
			if err != nil {
				slog.Warn("failed to attach shell", "error", err)
			}

			_ = req.Reply(err == nil, nil)
		case "exec":
			slog.Debug("ignored exec", "payload", req.Payload)
		default:
			slog.Debug("unknown request", "type", req.Type, "reply", req.WantReply, "data", req.Payload)
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

func (s *sshServer) run(password string, callable starlark.Callable) error {
	s.callable = callable

	listener, err := net.Listen("tcp", "0.0.0.0:2222")
	if err != nil {
		return fmt.Errorf("ssh: failed to listen for connection: %v", err)
	}

	config := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			// Should use constant-time compare (or better, salt+hash) in
			// a production setting.
			if string(pass) == password {
				return nil, nil
			}
			return nil, fmt.Errorf("password rejected for %q", c.User())
		},
	}

	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("ssh: failed to generate key: %v", err)
	}

	hostSigner, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		return fmt.Errorf("ssh: failed to make signer: %v", err)
	}

	config.AddHostKey(hostSigner)

	for {
		nConn, err := listener.Accept()
		if err != nil {
			return err
		}
		go func() {
			err := s.handleClient(nConn, config)
			if err != nil {
				slog.Warn("failed to handle ssh client", "err", err)
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

// From: https://stackoverflow.com/questions/12518876/how-to-check-if-a-file-exists-in-go
func exists(name string) (bool, error) {
	_, err := os.Stat(name)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

type mountOptions struct {
	Readonly bool
}

func mount(kind string, mountName string, mountPoint string, opts mountOptions) error {
	var flags uintptr
	if opts.Readonly {
		flags |= unix.MS_RDONLY
	}
	err := unix.Mount(mountName, mountPoint, kind, flags, "")
	if err != nil {
		return fmt.Errorf("failed mounting %s(%s) on %s: %v", mountName, kind, mountPoint, err)
	}
	return nil
}

func ensure(path string, mode os.FileMode) error {
	exists, err := exists(path)
	if err != nil {
		return fmt.Errorf("failed to check for path: %v", err)
	}

	if !exists {
		err := os.Mkdir(path, mode)
		if err != nil {
			return fmt.Errorf("failed to create directory: %v", err)
		}
	}

	return nil
}

var (
	execShell = flag.Bool("shell", false, "start the shell instead of running /init.sh")
)

func initMain() error {
	flag.Parse()
	if *execShell {
		return shellMain()
	}

	var args starlark.Value = starlark.NewDict(0)

	if ok, _ := exists("/init.json"); ok {
		contents, err := os.ReadFile("/init.json")
		if err != nil {
			return err
		}

		args, err = starlarkJsonDecode(nil, starlark.Tuple{starlark.String(contents)}, []starlark.Tuple{})
		if err != nil {
			return err
		}
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

		slog.Info("configured networking statically", "routers", router)

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
			err := ensure(mountPoint, os.ModePerm)

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

		if err := ensure(path, os.ModePerm); err != nil {
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

	globals["run_ssh_server"] = starlark.NewBuiltin("run_ssh_server", func(
		thread *starlark.Thread,
		fn *starlark.Builtin,
		args starlark.Tuple,
		kwargs []starlark.Tuple,
	) (starlark.Value, error) {
		var (
			callable starlark.Callable
		)

		if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
			"callable", &callable,
		); err != nil {
			return starlark.None, err
		}

		sshServer := &sshServer{}

		err := sshServer.run("insecurepassword", callable)
		if err != nil {
			return starlark.None, err
		}

		return starlark.None, nil
	})

	thread := &starlark.Thread{Name: "init"}

	decls, err := starlark.ExecFileOptions(&syntax.FileOptions{Set: true, While: true, TopLevelControl: true}, thread, "/init.star", nil, globals)
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

func main() {
	if err := initMain(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

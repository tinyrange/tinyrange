package main

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"

	"github.com/insomniacslk/dhcp/netboot"
	"github.com/jsimonetti/rtnetlink/rtnl"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
	"golang.org/x/sys/unix"
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

func initMain() error {
	globals := starlark.StringDict{}

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

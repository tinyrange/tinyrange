package main

import (
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"

	"github.com/insomniacslk/dhcp/netboot"
	"github.com/jsimonetti/rtnetlink/rtnl"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

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

package main

import (
	_ "embed"
	"errors"
	"flag"
	"fmt"
	goFs "io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/miekg/dns"
	"github.com/tinyrange/tinyrange/pkg/filesystem/ext4"
	"github.com/tinyrange/tinyrange/pkg/netstack"
	"github.com/tinyrange/tinyrange/pkg/oci"
	virtualMachine "github.com/tinyrange/tinyrange/pkg/vm"
	gonbd "github.com/tinyrange/tinyrange/third_party/go-nbd"
	"github.com/tinyrange/vm"
)

//go:embed init.star
var _INIT_SCRIPT []byte

type vmBackend struct {
	vm *vm.VirtualMemory
}

// Close implements common.Backend.
func (vm *vmBackend) Close() error {
	return nil
}

// PreferredBlockSize implements common.Backend.
func (*vmBackend) PreferredBlockSize() int64 { return 4096 }

// ReadAt implements common.Backend.
func (vm *vmBackend) ReadAt(p []byte, off int64) (n int, err error) {
	n, err = vm.vm.ReadAt(p, off)
	if err != nil {
		slog.Info("vmBackend readAt", "len", len(p), "off", off, "err", err)
		return 0, nil
	}

	return
}

// WriteAt implements common.Backend.
func (vm *vmBackend) WriteAt(p []byte, off int64) (n int, err error) {
	n, err = vm.vm.WriteAt(p, off)
	if err != nil {
		slog.Info("vmBackend writeAt", "len", len(p), "off", off, "err", err)
		return 0, nil
	}

	return
}

// Size implements common.Backend.
func (vm *vmBackend) Size() (int64, error) {
	return vm.vm.Size(), nil
}

// Sync implements common.Backend.
func (*vmBackend) Sync() error {
	return nil
}

var (
	storageSize = flag.Int("storage-size", 64, "the size of the VM storage in megabytes")
	image       = flag.String("image", "library/alpine:latest", "the OCI image to boot inside the virtual machine")
)

func tinyRangeMain() error {
	flag.Parse()

	fsSize := int64(*storageSize * 1024 * 1024)

	vmem := vm.NewVirtualMemory(fsSize, 4096)

	fs, err := ext4.CreateExt4Filesystem(vmem, 0, fsSize)
	if err != nil {
		return err
	}

	initExe, err := os.Open("build/init_x86_64")
	if err != nil {
		return err
	}
	defer initExe.Close()

	initRegion, err := vm.NewFileRegion(initExe)
	if err != nil {
		return err
	}

	if err := fs.CreateFile("/init", initRegion); err != nil {
		return err
	}
	if err := fs.Chmod("/init", goFs.FileMode(0755)); err != nil {
		return err
	}

	if err := fs.CreateFile("/init.star", vm.RawRegion(_INIT_SCRIPT)); err != nil {
		return err
	}
	if err := fs.Chmod("/init.star", goFs.FileMode(0755)); err != nil {
		return err
	}

	ociDl := oci.NewDownloader()

	if err := ociDl.ExtractOciImage(fs, *image); err != nil {
		return err
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("failed to listen: %v", err)
	}

	backend := &vmBackend{vm: vmem}

	go func() {
		for {
			conn, err := listener.Accept()
			if errors.Is(err, net.ErrClosed) {
				return
			} else if err != nil {
				slog.Error("nbd server failed to accept", "error", err)
				return
			}

			go func(conn net.Conn) {
				slog.Debug("got nbd connection", "remote", conn.RemoteAddr().String())
				err = gonbd.Handle(conn, []gonbd.Export{{
					Name:        "",
					Description: "",
					Backend:     backend,
				}}, &gonbd.Options{
					ReadOnly:           false,
					MinimumBlockSize:   1024,
					PreferredBlockSize: uint32(backend.PreferredBlockSize()),
					MaximumBlockSize:   32*1024*1024 - 1,
				})
				if err != nil {
					slog.Warn("nbd server failed to handle", "error", err)
				}
			}(conn)
		}
	}()

	ns := netstack.New()

	out, err := os.Create("local/network.pcap")
	if err != nil {
		return err
	}
	defer out.Close()

	ns.OpenPacketCapture(out)

	go func() {
		// TODO(joshua): Fix this horrible hack.
		time.Sleep(100 * time.Millisecond)

		listen, err := ns.ListenInternal("tcp", ":80")
		if err != nil {
			slog.Error("failed to listen", "err", err)
			return
		}

		mux := http.NewServeMux()

		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("Hello, World\n"))
		})

		go func() {
			slog.Error("failed to serve", "err", http.Serve(listen, mux))
		}()

		dnsServer := &dnsServer{
			dnsLookup: func(name string) (string, error) {
				if name == "host.internal." {
					return "10.42.0.1", nil
				}

				slog.Info("doing DNS lookup", "name", name)

				// Do a DNS lookup on the host.
				addr, err := net.ResolveIPAddr("ip4", name)
				if err != nil {
					return "", err
				}

				return string(addr.IP.String()), nil
			},
		}

		dnsMux := dns.NewServeMux()

		dnsMux.HandleFunc(".", dnsServer.handleDnsRequest)

		packetConn, err := ns.ListenPacketInternal("udp", ":53")
		if err != nil {
			slog.Error("dns: failed to create packet connection", "err", err)
		}

		dnsServer.server = &dns.Server{
			Addr:       ":53",
			Net:        "udp",
			Handler:    dnsMux,
			PacketConn: packetConn,
		}

		go func() {
			err := dnsServer.server.ActivateAndServe()
			if err != nil {
				slog.Error("dns: failed to start server", "error", err.Error())
			}
		}()

	}()

	factory, err := virtualMachine.LoadVirtualMachineFactory("hv/qemu/qemu.star")
	if err != nil {
		return err
	}

	virtualMachine, err := factory.Create(
		"local/vmlinux_x86_64",
		"",
		"nbd://"+listener.Addr().String(),
		ns,
	)
	if err != nil {
		return err
	}

	go func() {
		if err := virtualMachine.Run(false); err != nil {
			slog.Error("failed to run virtual machine", "err", err)
		}
	}()
	defer virtualMachine.Shutdown()

	err = connectOverSsh(ns, "10.42.0.2:2222", "root", "insecurepassword")
	if err != nil {
		return err
	}

	// for {
	// 	time.Sleep(1 * time.Hour)
	// }

	return nil
}

func main() {
	if err := tinyRangeMain(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

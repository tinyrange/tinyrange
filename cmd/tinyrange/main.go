package main

import (
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	goFs "io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/miekg/dns"
	"github.com/tinyrange/tinyrange/pkg/archive"
	"github.com/tinyrange/tinyrange/pkg/config"
	"github.com/tinyrange/tinyrange/pkg/filesystem/ext4"
	"github.com/tinyrange/tinyrange/pkg/netstack"
	"github.com/tinyrange/tinyrange/pkg/oci"
	virtualMachine "github.com/tinyrange/tinyrange/pkg/vm"
	gonbd "github.com/tinyrange/tinyrange/third_party/go-nbd"
	"github.com/tinyrange/vm"
	"gopkg.in/yaml.v3"
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

func runWithConfig(cfg config.TinyRangeConfig, debug bool) error {
	if cfg.StorageSize == 0 {
		return fmt.Errorf("invalid config")
	}

	slog.Info("starting TinyRange")

	fsSize := int64(cfg.StorageSize * 1024 * 1024)

	vmem := vm.NewVirtualMemory(fsSize, 4096)

	fs, err := ext4.CreateExt4Filesystem(vmem, 0, fsSize)
	if err != nil {
		return err
	}

	for _, frag := range cfg.RootFsFragments {
		if localFile := frag.LocalFile; localFile != nil {
			file, err := os.Open(cfg.Resolve(localFile.HostFilename))
			if err != nil {
				return err
			}

			region, err := vm.NewFileRegion(file)
			if err != nil {
				return err
			}

			if err := fs.CreateFile(localFile.GuestFilename, region); err != nil {
				return err
			}

			if localFile.Executable {
				if err := fs.Chmod(localFile.GuestFilename, goFs.FileMode(0755)); err != nil {
					return err
				}
			}
		} else if fileContents := frag.FileContents; fileContents != nil {
			if err := fs.CreateFile(fileContents.GuestFilename, vm.RawRegion(fileContents.Contents)); err != nil {
				return err
			}

			if fileContents.Executable {
				if err := fs.Chmod(fileContents.GuestFilename, goFs.FileMode(0755)); err != nil {
					return err
				}
			}
		} else if ociImage := frag.OCIImage; ociImage != nil {
			ociDl := oci.NewDownloader()

			if err := ociDl.ExtractOciImage(fs, ociImage.ImageName); err != nil {
				return err
			}
		} else if ark := frag.Archive; ark != nil {
			fh, err := os.Open(cfg.Resolve(ark.HostFilename))
			if err != nil {
				return err
			}

			entries, err := archive.ReadArchiveFromFile(fh)
			if err != nil {
				return err
			}

			for _, ent := range entries {
				name := "/" + ent.CName

				if fs.Exists(name) {
					continue
				}

				switch ent.CTypeflag {
				case archive.TypeDirectory:
					name = strings.TrimSuffix(name, "/")
					if err := fs.Mkdir(name, true); err != nil {
						return err
					}
				case archive.TypeSymlink:
					if err := fs.Symlink(name, ent.CLinkname); err != nil {
						return err
					}
				case archive.TypeLink:
					if err := fs.Link(name, "/"+ent.CLinkname); err != nil {
						return err
					}
				case archive.TypeRegular:
					f, err := ent.Open()
					if err != nil {
						return err
					}

					region, err := vm.NewFileRegion(f)
					if err != nil {
						return err
					}

					if err := fs.CreateFile(name, region); err != nil {
						return err
					}
				default:
					return fmt.Errorf("unimplemented entry type: %s", ent.CTypeflag)
				}

				if err := fs.Chown(name, uint16(ent.CUid), uint16(ent.CGid)); err != nil {
					return err
				}

				if err := fs.Chmod(name, goFs.FileMode(ent.CMode)); err != nil {
					return err
				}
			}
		} else {
			return fmt.Errorf("unknown fragment kind")
		}
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

	// out, err := os.Create("local/network.pcap")
	// if err != nil {
	// 	return err
	// }
	// defer out.Close()

	// ns.OpenPacketCapture(out)

	factory, err := virtualMachine.LoadVirtualMachineFactory(cfg.Resolve(cfg.HypervisorScript))
	if err != nil {
		return err
	}

	virtualMachine, err := factory.Create(
		cfg.Resolve(cfg.KernelFilename),
		cfg.Resolve(cfg.InitFilesystemFilename),
		"nbd://"+listener.Addr().String(),
	)
	if err != nil {
		return err
	}

	nic, err := ns.AttachNetworkInterface()
	if err != nil {
		return err
	}

	// Create internal HTTP server.
	{
		listen, err := ns.ListenInternal("tcp", ":80")
		if err != nil {
			return err
		}

		mux := http.NewServeMux()

		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("Hello, World\n"))
		})

		go func() {
			slog.Error("failed to serve", "err", http.Serve(listen, mux))
		}()
	}

	// Create DNS server.
	{
		dnsServer := &dnsServer{
			dnsLookup: func(name string) (string, error) {
				if name == "host.internal." {
					return "10.42.0.1", nil
				}

				slog.Debug("doing DNS lookup", "name", name)

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
			return err
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
	}

	slog.Info("Starting virtual machine.")

	go func() {
		if err := virtualMachine.Run(nic, debug); err != nil {
			slog.Error("failed to run virtual machine", "err", err)
		}
	}()
	defer virtualMachine.Shutdown()

	// Start a loop so SSH can be restarted when requested by the user.
	for {
		err = connectOverSsh(ns, "10.42.0.2:2222", "root", "insecurepassword")
		if err == ErrRestart {
			continue
		} else if err != nil {
			return err
		}

		return nil
	}
}

var (
	storageSize = flag.Int("storage-size", 64, "the size of the VM storage in megabytes")
	image       = flag.String("image", "library/alpine:latest", "the OCI image to boot inside the virtual machine")
	configFile  = flag.String("config", "", "passes a custom config. this overrides all other flags.")
	debug       = flag.Bool("debug", false, "redirect output from the hypervisor to the host. the guest will exit as soon as the VM finishes startup")
)

func tinyRangeMain() error {
	flag.Parse()

	if *configFile != "" {
		f, err := os.Open(*configFile)
		if err != nil {
			return err
		}
		defer f.Close()

		var cfg config.TinyRangeConfig

		if strings.HasSuffix(f.Name(), ".json") {

			dec := json.NewDecoder(f)

			if err := dec.Decode(&cfg); err != nil {
				return err
			}
		} else if strings.HasSuffix(f.Name(), ".yml") {
			dec := yaml.NewDecoder(f)

			if err := dec.Decode(&cfg); err != nil {
				return err
			}
		}

		return runWithConfig(cfg, *debug)
	} else {
		return runWithConfig(config.TinyRangeConfig{
			HypervisorScript: "hv/qemu/qemu.star",
			KernelFilename:   "local/vmlinux_x86_64",
			RootFsFragments: []config.Fragment{
				{LocalFile: &config.LocalFileFragment{HostFilename: "build/init_x86_64", GuestFilename: "/init", Executable: true}},
				{FileContents: &config.FileContentsFragment{Contents: _INIT_SCRIPT, GuestFilename: "/init.star"}},
				{OCIImage: &config.OCIImageFragment{ImageName: *image}},
			},
			StorageSize: *storageSize,
		}, *debug)
	}
}

func main() {
	if err := tinyRangeMain(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

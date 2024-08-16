package tinyrange

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	goFs "io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"github.com/miekg/dns"
	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/config"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/filesystem/ext4"
	initExec "github.com/tinyrange/tinyrange/pkg/init"
	"github.com/tinyrange/tinyrange/pkg/netstack"
	_ "github.com/tinyrange/tinyrange/pkg/platform"
	virtualMachine "github.com/tinyrange/tinyrange/pkg/vm"
	gonbd "github.com/tinyrange/tinyrange/third_party/go-nbd"
	"github.com/tinyrange/vm"
)

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
		slog.Error("vmBackend readAt", "len", len(p), "off", off, "err", err)
		return 0, nil
	}

	return
}

// WriteAt implements common.Backend.
func (vm *vmBackend) WriteAt(p []byte, off int64) (n int, err error) {
	n, err = vm.vm.WriteAt(p, off)
	if err != nil {
		slog.Error("vmBackend writeAt", "len", len(p), "off", off, "err", err)
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

func RunWithConfig(
	buildDir string,
	cfg config.TinyRangeConfig,
	debug bool,
	forwardSsh bool,
	exportFilesystem string,
	listenNbd string,
) error {
	if cfg.StorageSize == 0 || cfg.CPUCores == 0 || cfg.MemoryMB == 0 {
		return fmt.Errorf("invalid config")
	}

	if cfg.Debug {
		debug = true
	}

	interaction := cfg.Interaction
	if interaction == "" {
		interaction = "ssh"
	}

	fsSize := int64(cfg.StorageSize * 1024 * 1024)

	vmem := vm.NewVirtualMemory(fsSize, 4096)

	start := time.Now()

	fs, err := ext4.CreateExt4Filesystem(vmem, 0, fsSize)
	if err != nil {
		return fmt.Errorf("failed to create ext4 filesystem: %w", err)
	}

	var exportedPorts []int

	for _, frag := range cfg.RootFsFragments {
		if localFile := frag.LocalFile; localFile != nil {
			file, err := os.Open(cfg.Resolve(localFile.HostFilename))
			if err != nil {
				return fmt.Errorf("failed to load local file: %w", err)
			}

			region, err := vm.NewFileRegion(file)
			if err != nil {
				return fmt.Errorf("failed to create file region: %w", err)
			}

			dirName := path.Dir(localFile.GuestFilename)

			if !fs.Exists(dirName) {
				if err := fs.Mkdir(dirName, true); err != nil {
					return fmt.Errorf("failed to mkdir %s: %w", dirName, err)
				}
			}

			if err := fs.CreateFile(localFile.GuestFilename, region); err != nil {
				return fmt.Errorf("failed to create file %s: %w", localFile.GuestFilename, err)
			}

			if localFile.Executable {
				if err := fs.Chmod(localFile.GuestFilename, goFs.FileMode(0755)); err != nil {
					return err
				}
			}
		} else if fileContents := frag.FileContents; fileContents != nil {
			dirName := path.Dir(fileContents.GuestFilename)

			if !fs.Exists(dirName) {
				if err := fs.Mkdir(dirName, true); err != nil {
					return err
				}
			}

			if err := fs.CreateFile(fileContents.GuestFilename, vm.RawRegion(fileContents.Contents)); err != nil {
				return err
			}

			if fileContents.Executable {
				if err := fs.Chmod(fileContents.GuestFilename, goFs.FileMode(0755)); err != nil {
					return err
				}
			}
		} else if builtin := frag.Builtin; builtin != nil {
			dirName := path.Dir(builtin.GuestFilename)

			if !fs.Exists(dirName) {
				if err := fs.Mkdir(dirName, true); err != nil {
					return err
				}
			}

			if builtin.Name == "init" {
				if err := fs.CreateFile(builtin.GuestFilename, vm.RawRegion(initExec.INIT_EXECUTABLE)); err != nil {
					return err
				}

				if err := fs.Chmod(builtin.GuestFilename, goFs.FileMode(0755)); err != nil {
					return err
				}
			} else if builtin.Name == "init.star" {
				if err := fs.CreateFile(builtin.GuestFilename, vm.RawRegion(initExec.INIT_SCRIPT)); err != nil {
					return err
				}
			} else {
				return fmt.Errorf("unknown builtin: %s", builtin.Name)
			}
		} else if ark := frag.Archive; ark != nil {
			f := filesystem.NewLocalFile(cfg.Resolve(ark.HostFilename), nil)

			archive, err := filesystem.ReadArchiveFromFile(f)
			if err != nil {
				return fmt.Errorf("failed to read archive: %w", err)
			}

			entries, err := archive.Entries()
			if err != nil {
				return fmt.Errorf("failed to read archive: %w", err)
			}

			for _, ent := range entries {
				name := ark.Target + "/" + ent.Name()

				if fs.Exists(name) {
					continue
				}

				switch ent.Typeflag() {
				case filesystem.TypeDirectory:
					// slog.Info("directory", "name", name)
					name = strings.TrimSuffix(name, "/")
					if err := fs.Mkdir(name, true); err != nil {
						return fmt.Errorf("failed to mkdir in guest: %w", err)
					}
				case filesystem.TypeSymlink:
					// slog.Info("symlink", "name", name)
					if err := fs.Symlink(name, ent.Linkname()); err != nil {
						return fmt.Errorf("failed to symlink in guest: %w", err)
					}
				case filesystem.TypeLink:
					// slog.Info("link", "name", name)
					if err := fs.Link(name, "/"+ent.Linkname()); err != nil {
						return fmt.Errorf("failed to link in guest: %w", err)
					}
				case filesystem.TypeRegular:
					// slog.Info("reg", "name", name)
					f, err := ent.Open()
					if err != nil {
						return fmt.Errorf("failed to open file for guest: %w", err)
					}

					region := vm.NewReaderRegion(f, ent.Size())

					if err := fs.CreateFile(name, region); err != nil {
						return fmt.Errorf("failed to create file in guest %s: %w", name, err)
					}
				default:
					return fmt.Errorf("unimplemented entry type: %s", ent.Typeflag())
				}

				if err := fs.Chown(name, uint16(ent.Uid()), uint16(ent.Gid())); err != nil {
					return fmt.Errorf("failed to chown in guest: %w", err)
				}

				if err := fs.Chmod(name, goFs.FileMode(ent.Mode())); err != nil {
					return fmt.Errorf("failed to chmod in guest: %w", err)
				}
			}
		} else if port := frag.ExportPort; port != nil {
			exportedPorts = append(exportedPorts, port.Port)
		} else {
			return fmt.Errorf("unknown fragment kind")
		}
	}

	slog.Debug("built filesystem", "took", time.Since(start))

	if exportFilesystem != "" {
		out, err := os.Create(exportFilesystem)
		if err != nil {
			return err
		}
		defer out.Close()

		if _, err := io.Copy(out, io.NewSectionReader(vmem, 0, fsSize)); err != nil {
			return err
		}

		return nil
	}

	if listenNbd != "" {
		listener, err := net.Listen("tcp", listenNbd)
		if err != nil {
			return fmt.Errorf("failed to listen: %v", err)
		}

		slog.Info("nbd listening on", "addr", listener.Addr().String())

		backend := &vmBackend{vm: vmem}

		for {
			conn, err := listener.Accept()
			if errors.Is(err, net.ErrClosed) {
				return nil
			} else if err != nil {
				return err
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
	}

	start = time.Now()

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

	factory, err := virtualMachine.LoadVirtualMachineFactory(buildDir, cfg.Resolve(cfg.HypervisorScript))
	if err != nil {
		return fmt.Errorf("failed to load virtual machine factory: %w", err)
	}

	virtualMachine, err := factory.Create(
		cfg.CPUCores,
		cfg.MemoryMB,
		cfg.Resolve(cfg.KernelFilename),
		cfg.Resolve(cfg.InitFilesystemFilename),
		"nbd://"+listener.Addr().String(),
	)
	if err != nil {
		return fmt.Errorf("failed to make virtual machine: %w", err)
	}

	nic, err := ns.AttachNetworkInterface()
	if err != nil {
		return fmt.Errorf("failed to attach network interface: %w", err)
	}

	// Create internal HTTP server.
	{
		listen, err := ns.ListenInternal("tcp", ":80")
		if err != nil {
			return fmt.Errorf("failed to listen internal: %w", err)
		}

		mux := http.NewServeMux()

		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", fmt.Sprintf("%d", 4096*1024*1024))
			io.CopyN(w, rand.Reader, 4096*1024*1024)
		})

		go func() {
			slog.Error("failed to serve", "err", http.Serve(listen, mux))
		}()
	}

	// Create DNS server.
	{
		dnsServer := &dnsServer{
			dnsLookup: func(name string) (string, error) {
				if name == "tinyrange." {
					return "10.42.0.2", nil
				} else if name == "host.internal." {
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
			return fmt.Errorf("failed to listen internal (dns): %w", err)
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

	// Create forwarder for SSH connection.
	if forwardSsh {
		sshListen, err := net.Listen("tcp", "localhost:2222")
		if err != nil {
			return err
		}

		go func() {
			for {
				conn, err := sshListen.Accept()
				if err != nil {
					slog.Error("failed to accept", "err", err)
					return
				}

				go func() {
					defer conn.Close()

					clientConn, err := ns.DialInternalContext(context.Background(), "tcp", "10.42.0.2:2222")
					if err != nil {
						slog.Error("failed to dial vm ssh", "err", err)
						return
					}
					defer clientConn.Close()

					if err := common.Proxy(clientConn, conn, 4096); err != nil {
						slog.Error("failed to proxy ssh connection", "err", err)
						return
					}
				}()
			}
		}()
	}

	for _, port := range exportedPorts {
		portListen, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", port))
		if err != nil {
			return err
		}

		go func() {
			for {
				conn, err := portListen.Accept()
				if err != nil {
					slog.Error("failed to accept", "err", err)
					return
				}

				go func() {
					defer conn.Close()

					clientConn, err := ns.DialInternalContext(context.Background(), "tcp", fmt.Sprintf("10.42.0.2:%d", port))
					if err != nil {
						slog.Error("failed to dial vm port", "err", err)
						return
					}
					defer clientConn.Close()

					if err := common.Proxy(clientConn, conn, 4096); err != nil {
						slog.Error("failed to proxy connection", "err", err)
						return
					}
				}()
			}
		}()
	}

	slog.Debug("starting virtual machine", "took", time.Since(start))

	if interaction == "ssh" || interaction == "vnc" {
		go func() {
			if err := virtualMachine.Run(nic, debug); err != nil {
				slog.Error("failed to run virtual machine", "err", err)
			}
		}()
		defer virtualMachine.Shutdown()

		// return nil

		if interaction == "vnc" {
			go runVncClient(ns, "10.42.0.2:5901")
		}

		// Start a loop so SSH can be restarted when requested by the user.
		for {
			err = connectOverSsh(ns, "10.42.0.2:2222", "root", "insecurepassword")
			if err == ErrRestart {
				continue
			} else if err != nil {
				return fmt.Errorf("failed to connect over ssh: %w", err)
			}

			return nil
		}
	} else if interaction == "serial" {
		if err := virtualMachine.Run(nic, true); err != nil {
			slog.Error("failed to run virtual machine", "err", err)
		}
		defer virtualMachine.Shutdown()

		return nil
	} else {
		return fmt.Errorf("unknown interaction: %s", interaction)
	}
}

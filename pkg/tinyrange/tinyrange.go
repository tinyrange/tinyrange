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

type TinyRange struct {
	buildDir           string
	cfg                config.TinyRangeConfig
	debug              bool
	forwardSsh         bool
	exportFilesystem   string
	listenNbd          string
	streamingServer    string
	client             *http.Client
	deferredFilesystem []func() error
}

func (tr *TinyRange) fragmentToFilesystem(frag config.Fragment, dir filesystem.MutableDirectory) error {
	if localFile := frag.LocalFile; localFile != nil {
		file := filesystem.NewLocalFile(tr.cfg.Resolve(localFile.HostFilename), nil)

		overlay, err := filesystem.NewOverlayFile(file)
		if err != nil {
			return err
		}

		if localFile.Executable {
			if err := overlay.Chmod(goFs.FileMode(0755)); err != nil {
				return err
			}
		}

		if err := filesystem.CreateChild(dir, localFile.GuestFilename, overlay); err != nil {
			return err
		}

		return nil
	} else if fileContents := frag.FileContents; fileContents != nil {
		file := filesystem.NewMemoryFile(filesystem.TypeRegular)

		if err := file.Overwrite(fileContents.Contents); err != nil {
			return err
		}

		if fileContents.Executable {
			if err := file.Chmod(goFs.FileMode(0755)); err != nil {
				return err
			}
		}

		if err := filesystem.CreateChild(dir, fileContents.GuestFilename, file); err != nil {
			return err
		}

		return nil
	} else if builtin := frag.Builtin; builtin != nil {
		if builtin.Name == "init" {
			exec, err := initExec.GetInitExecutable(builtin.Architecture)
			if err != nil {
				return err
			}

			file := filesystem.NewMemoryFile(filesystem.TypeRegular)

			if err := file.Overwrite(exec); err != nil {
				return err
			}

			if err := file.Chmod(goFs.FileMode(0755)); err != nil {
				return err
			}

			if err := filesystem.CreateChild(dir, builtin.GuestFilename, file); err != nil {
				return err
			}

			return nil
		} else if builtin.Name == "init.star" {
			file := filesystem.NewMemoryFile(filesystem.TypeRegular)

			if err := file.Overwrite(initExec.INIT_SCRIPT); err != nil {
				return err
			}

			if err := filesystem.CreateChild(dir, builtin.GuestFilename, file); err != nil {
				return err
			}

			return nil
		} else if builtin.Name == "tinyrange" {
			exe, err := os.Executable()
			if err != nil {
				return fmt.Errorf("failed to get executable: %w", err)
			}

			file := filesystem.NewLocalFile(exe, nil)

			if err := filesystem.CreateChild(dir, builtin.GuestFilename, file); err != nil {
				return err
			}

			return nil
		} else if builtin.Name == "tinyrange_qemu.star" {
			local, err := common.GetAdjacentExecutable("tinyrange_qemu.star")
			if err != nil {
				return fmt.Errorf("failed to get tinyrange_qemu.star: %w", err)
			}

			file := filesystem.NewLocalFile(local, nil)

			if err := filesystem.CreateChild(dir, builtin.GuestFilename, file); err != nil {
				return err
			}

			return nil
		} else {
			return fmt.Errorf("unknown builtin: %s", builtin.Name)
		}
	} else if ark := frag.Archive; ark != nil {
		var (
			archive filesystem.Archive
			err     error
		)

		if tr.streamingServer != "" {
			f := filesystem.NewRemoteFile(tr.client, tr.streamingServer+ark.HostFilename)

			archive, err = filesystem.ReadArchiveFromStreamingServer(tr.client, tr.streamingServer, f)
			if err != nil {
				return fmt.Errorf("failed to download archive: %w", err)
			}
		} else {
			f := filesystem.NewLocalFile(tr.cfg.Resolve(ark.HostFilename), nil)

			archive, err = filesystem.ReadArchiveFromFile(f)
			if err != nil {
				return fmt.Errorf("failed to read archive: %w", err)
			}
		}

		entries, err := archive.Entries()
		if err != nil {
			return fmt.Errorf("failed to read archive: %w", err)
		}

		for _, ent := range entries {
			// TODO(joshua): Why is this not filepath.join?
			name := ark.Target + "/" + ent.Name()

			var file filesystem.MutableFile

			if name != "/" {
				if filesystem.Exists(dir, name) {
					continue
				}

				dirname := path.Dir(name)

				if !filesystem.Exists(dir, dirname) && path.Clean(name) != dirname {
					// slog.Info("mkdir", "dirname", dirname)
					if _, err := filesystem.Mkdir(dir, dirname); err != nil {
						return err
					}
				}

				switch ent.Typeflag() {
				case filesystem.TypeDirectory:
					// slog.Info("directory", "name", name)
					name = strings.TrimSuffix(name, "/")

					file, err = filesystem.Mkdir(dir, name)
					if err != nil {
						return err
					}
				case filesystem.TypeSymlink:
					// slog.Info("symlink", "name", name)
					symlink := filesystem.NewSymlink(ent.Linkname())

					file = symlink

					if err := filesystem.CreateChild(dir, name, symlink); err != nil {
						return err
					}
				case filesystem.TypeLink:
					// slog.Info("link", "name", name, "target", ent.Linkname())
					link, err := filesystem.NewHardLink(ent.Linkname())
					if err != nil {
						return err
					}

					file = link

					if err := filesystem.CreateChild(dir, name, link); err != nil {
						return err
					}
				case filesystem.TypeRegular:
					// slog.Info("reg", "name", name)
					file, err = filesystem.NewOverlayFile(ent)
					if err != nil {
						return err
					}

					if err := filesystem.CreateChild(dir, name, ent); err != nil {
						return err
					}
				default:
					return fmt.Errorf("unimplemented entry type: %s", ent.Typeflag())
				}
			} else {
				file = dir
			}

			if err := file.Chown(ent.Uid(), ent.Gid()); err != nil {
				return fmt.Errorf("failed to chown in guest: %w", err)
			}

			if err := file.Chmod(goFs.FileMode(ent.Mode())); err != nil {
				return fmt.Errorf("failed to chmod in guest: %w", err)
			}
		}

		return nil
	} else {
		return fmt.Errorf("unknown fragment kind")
	}
}

// Recurse into an filesystem.Directory and put all it's contents into a ext4 filesystem.
func (tr *TinyRange) filesystemToExt4(dir filesystem.Directory, fs *ext4.Ext4Filesystem, name string) error {
	ents, err := dir.Readdir()
	if err != nil {
		return fmt.Errorf("failed to readdir: %w", err)
	}

	for _, ent := range ents {
		info, err := ent.File.Stat()
		if err != nil {
			return fmt.Errorf("failed to stat: %w", err)
		}

		name := path.Join(name, path.Base(ent.Name))

		skip := false

		switch info.Kind() {
		case filesystem.TypeDirectory:
			if err := fs.Mkdir(name, false); err != nil {
				return fmt.Errorf("failed to mkdir %s: %w", name, err)
			}

			child, ok := ent.File.(filesystem.Directory)
			if !ok {
				return fmt.Errorf("directory does not implement Directory: %T", ent.File)
			}

			if err := tr.filesystemToExt4(child, fs, name); err != nil {
				return err
			}
		case filesystem.TypeLink:
			target, err := filesystem.GetLinkName(ent.File)
			if err != nil {
				return fmt.Errorf("failed to get linkname: %w", err)
			}

			if err := fs.Link(name, target); err != nil {
				tr.deferredFilesystem = append(tr.deferredFilesystem, func() error {
					if err := fs.Link(name, target); err != nil {
						return fmt.Errorf("failed to make hard link: %w", err)
					}

					if err := fs.Chmod(name, info.Mode()); err != nil {
						return fmt.Errorf("failed to chmod: %w", err)
					}

					uid, gid, err := filesystem.GetUidAndGid(ent.File)
					if err != nil {
						return fmt.Errorf("failed to GetUidAndGid: %w", err)
					}

					if err := fs.Chown(name, uint16(uid), uint16(gid)); err != nil {
						return fmt.Errorf("failed to chown: %w", err)
					}

					return nil
				})

				skip = true
			}
		case filesystem.TypeSymlink:
			target, err := filesystem.GetLinkName(ent.File)
			if err != nil {
				return fmt.Errorf("failed to get linkname: %w", err)
			}

			if err := fs.Symlink(name, target); err != nil {
				return fmt.Errorf("failed to make symlink: %w", err)
			}
		case filesystem.TypeRegular:
			f, err := ent.File.Open()
			if err != nil {
				return fmt.Errorf("failed to open file for guest: %T %w", ent.File, err)
			}

			region := vm.NewReaderRegion(f, info.Size())

			if err := fs.CreateFile(name, region); err != nil {
				return fmt.Errorf("failed to create file in guest %s: %w", name, err)
			}
		default:
			return fmt.Errorf("unimplemented kind: %s", info.Kind())
		}

		if !skip {
			if err := fs.Chmod(name, info.Mode()); err != nil {
				return fmt.Errorf("failed to chmod: %w", err)
			}

			uid, gid, err := filesystem.GetUidAndGid(ent.File)
			if err != nil {
				return fmt.Errorf("failed to GetUidAndGid: %w", err)
			}

			if err := fs.Chown(name, uint16(uid), uint16(gid)); err != nil {
				return fmt.Errorf("failed to chown: %w", err)
			}
		}
	}

	return nil
}

func (tr *TinyRange) runWithConfig() error {
	if tr.cfg.StorageSize == 0 || tr.cfg.CPUCores == 0 || tr.cfg.MemoryMB == 0 {
		return fmt.Errorf("invalid config")
	}

	if tr.cfg.Debug {
		slog.Warn("enabling hypervisor debug mode")
		tr.debug = true
	}

	interaction := tr.cfg.Interaction
	if interaction == "" {
		interaction = "ssh"
	}

	fsSize := int64(tr.cfg.StorageSize * 1024 * 1024)

	vmem := vm.NewVirtualMemory(fsSize, 4096)

	start := time.Now()

	var exportedPorts []int

	root := filesystem.NewMemoryDirectory()

	for _, frag := range tr.cfg.RootFsFragments {
		if port := frag.ExportPort; port != nil {
			exportedPorts = append(exportedPorts, port.Port)
		} else {
			if err := tr.fragmentToFilesystem(frag, root); err != nil {
				return fmt.Errorf("failed to extract fragment to filesystem: %w", err)
			}
		}
	}

	slog.Debug("built filesystem tree", "took", time.Since(start))

	start = time.Now()

	fs, err := ext4.CreateExt4Filesystem(vmem, 0, fsSize)
	if err != nil {
		return fmt.Errorf("failed to create ext4 filesystem: %w", err)
	}

	if err := tr.filesystemToExt4(root, fs, "/"); err != nil {
		return fmt.Errorf("failed to convert filesystem to ext4: %w", err)
	}

	for _, deferred := range tr.deferredFilesystem {
		if err := deferred(); err != nil {
			return err
		}
	}

	slog.Debug("built filesystem", "took", time.Since(start))

	if tr.exportFilesystem != "" {
		start := time.Now()

		out, err := os.Create(tr.exportFilesystem)
		if err != nil {
			return err
		}
		defer out.Close()

		if _, err := io.Copy(out, io.NewSectionReader(vmem, 0, fsSize)); err != nil {
			return err
		}

		slog.Debug("exported filesystem", "took", time.Since(start))

		return nil
	}

	if tr.listenNbd != "" {
		listener, err := net.Listen("tcp", tr.listenNbd)
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

	factory, err := virtualMachine.LoadVirtualMachineFactory(tr.buildDir, tr.cfg.Resolve(tr.cfg.HypervisorScript))
	if err != nil {
		return fmt.Errorf("failed to load virtual machine factory: %w", err)
	}

	virtualMachine, err := factory.Create(
		tr.cfg.CPUCores,
		tr.cfg.MemoryMB,
		tr.cfg.Architecture,
		tr.cfg.Resolve(tr.cfg.KernelFilename),
		tr.cfg.Resolve(tr.cfg.InitFilesystemFilename),
		"nbd://"+listener.Addr().String(),
		tr.cfg.Interaction,
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
	if tr.forwardSsh {
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
			if err := virtualMachine.Run(nic, tr.debug); err != nil {
				slog.Error("failed to run virtual machine", "err", err)
				os.Exit(1)
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
			return err
		}
		defer virtualMachine.Shutdown()

		return nil
	} else {
		return fmt.Errorf("unknown interaction: %s", interaction)
	}
}

func RunWithConfig(
	buildDir string,
	cfg config.TinyRangeConfig,
	debug bool,
	forwardSsh bool,
	exportFilesystem string,
	listenNbd string,
	streamingServer string,
) error {
	tr := &TinyRange{
		buildDir:         buildDir,
		cfg:              cfg,
		debug:            debug,
		forwardSsh:       forwardSsh,
		exportFilesystem: exportFilesystem,
		listenNbd:        listenNbd,
		streamingServer:  streamingServer,
		client:           http.DefaultClient,
	}

	return tr.runWithConfig()
}

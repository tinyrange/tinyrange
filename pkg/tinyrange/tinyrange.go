package tinyrange

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	goFs "io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/miekg/dns"
	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/config"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/filesystem/ext4"
	initExec "github.com/tinyrange/tinyrange/pkg/init"
	"github.com/tinyrange/tinyrange/pkg/netstack"
	_ "github.com/tinyrange/tinyrange/pkg/platform"
	"github.com/tinyrange/tinyrange/pkg/sftp"
	virtualMachine "github.com/tinyrange/tinyrange/pkg/vm"
	gonbd "github.com/tinyrange/tinyrange/third_party/go-nbd"
	"github.com/tinyrange/vm"
	"golang.org/x/crypto/ssh"
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
	configs            []config.TinyRangeConfig
	debug              bool
	forwardSsh         bool
	exportFilesystem   string
	listenNbd          string
	streamingServer    string
	wireguardUrl       string
	secureSSH          string
	client             *http.Client
	deferredFilesystem []func() error
	onExit             []func()
	persistPath        string
}

func (tr *TinyRange) fragmentToFilesystem(cfg config.TinyRangeConfig, frag config.Fragment, dir filesystem.MutableDirectory) error {
	if localFile := frag.LocalFile; localFile != nil {
		file := filesystem.NewLocalFile(cfg.Resolve(localFile.HostFilename), nil)

		overlay, err := filesystem.NewOverlayFile(file)
		if err != nil {
			return err
		}

		if localFile.Executable {
			if err := overlay.Chmod(goFs.FileMode(0755)); err != nil {
				return err
			}
		}

		if _, err := filesystem.CreateChild(dir, localFile.GuestFilename, overlay); err != nil {
			return err
		}

		return nil
	} else if fileContents := frag.FileContents; fileContents != nil {
		file := filesystem.NewMemoryFile(filesystem.TypeRegular)

		if fileContents.StringContents != "" {
			if err := file.Overwrite([]byte(fileContents.StringContents)); err != nil {
				return err
			}
		} else {
			if err := file.Overwrite(fileContents.Contents); err != nil {
				return err
			}
		}

		if fileContents.Executable {
			if err := file.Chmod(goFs.FileMode(0755)); err != nil {
				return err
			}
		}

		if _, err := filesystem.CreateChild(dir, fileContents.GuestFilename, file); err != nil {
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

			if _, err := filesystem.CreateChild(dir, builtin.GuestFilename, file); err != nil {
				return err
			}

			return nil
		} else if builtin.Name == "init.star" {
			file := filesystem.NewMemoryFile(filesystem.TypeRegular)

			if err := file.Overwrite(initExec.INIT_SCRIPT); err != nil {
				return err
			}

			if _, err := filesystem.CreateChild(dir, builtin.GuestFilename, file); err != nil {
				return err
			}

			return nil
		} else if builtin.Name == "tinyrange" {
			exe, err := os.Executable()
			if err != nil {
				return fmt.Errorf("failed to get executable: %w", err)
			}

			file := filesystem.NewLocalFile(exe, nil)

			if _, err := filesystem.CreateChild(dir, builtin.GuestFilename, file); err != nil {
				return err
			}

			return nil
		} else if builtin.Name == "tinyrange_qemu.star" {
			local, err := common.GetAdjacentExecutable("tinyrange_qemu.star")
			if err != nil {
				return fmt.Errorf("failed to get tinyrange_qemu.star: %w", err)
			}

			file := filesystem.NewLocalFile(local, nil)

			if _, err := filesystem.CreateChild(dir, builtin.GuestFilename, file); err != nil {
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
			f := filesystem.NewLocalFile(cfg.Resolve(ark.HostFilename), nil)

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

					if _, err := filesystem.CreateChild(dir, name, symlink); err != nil {
						return err
					}
				case filesystem.TypeLink:
					// slog.Info("link", "name", name, "target", ent.Linkname())
					link, err := filesystem.NewHardLink(ent.Linkname())
					if err != nil {
						return err
					}

					file = link

					if _, err := filesystem.CreateChild(dir, name, link); err != nil {
						return err
					}
				case filesystem.TypeRegular:
					// slog.Info("reg", "name", name)
					file, err = filesystem.NewOverlayFile(ent)
					if err != nil {
						return err
					}

					if _, err := filesystem.CreateChild(dir, name, ent); err != nil {
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
		return fmt.Errorf("unknown fragment kind: %+v", frag)
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

func (tr *TinyRange) generateOrLoadSecureSSH() (SecureSSHConfig, error) {
	var secureSSH SecureSSHConfig

	if ok, _ := common.Exists(tr.secureSSH); ok {
		f, err := os.Open(tr.secureSSH)
		if err != nil {
			return SecureSSHConfig{}, fmt.Errorf("failed to open secure ssh config: %w", err)
		}
		defer f.Close()

		if err := json.NewDecoder(f).Decode(&secureSSH); err != nil {
			return SecureSSHConfig{}, fmt.Errorf("failed to decode secure ssh config: %w", err)
		}
	} else {
		secureSSH.Password = uuid.NewString()

		// Generate a new host key.
		privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return SecureSSHConfig{}, fmt.Errorf("ssh: failed to generate key: %v", err)
		}

		block, err := ssh.MarshalPrivateKey(privateKey, "")
		if err != nil {
			return SecureSSHConfig{}, fmt.Errorf("ssh: failed to marshal private key: %v", err)
		}

		blockBytes := pem.EncodeToMemory(block)
		if blockBytes == nil {
			return SecureSSHConfig{}, fmt.Errorf("ssh: failed to encode private key")
		}

		secureSSH.HostKey = string(blockBytes)

		publicKey, err := ssh.NewPublicKey(&privateKey.PublicKey)
		if err != nil {
			return SecureSSHConfig{}, fmt.Errorf("ssh: failed to generate public key: %v", err)
		}

		secureSSH.PublicKey = string(ssh.MarshalAuthorizedKey(publicKey))

		// Write the config to disk.
		f, err := os.Create(tr.secureSSH)
		if err != nil {
			return SecureSSHConfig{}, fmt.Errorf("failed to create secure ssh config: %w", err)
		}
		defer f.Close()

		if err := json.NewEncoder(f).Encode(secureSSH); err != nil {
			return SecureSSHConfig{}, fmt.Errorf("failed to encode secure ssh config: %w", err)
		}
	}

	return secureSSH, nil
}

func (tr *TinyRange) createNbdListener(tryUnix bool) (string, net.Listener, error) {
	if (runtime.GOOS == "linux" || runtime.GOOS == "darwin" || runtime.GOOS == "windows") && tr.persistPath != "" && tryUnix {
		pid := os.Getpid()

		filename := fmt.Sprintf("%s.%d.nbd.sock", tr.persistPath, pid)

		listener, err := net.Listen("unix", filename)
		if err != nil {
			slog.Warn("failed to listen on unix socket", "error", err)
			return tr.createNbdListener(false)
		}

		tr.onExit = append(tr.onExit, func() {
			if err := listener.Close(); err != nil {
				slog.Error("failed to close listener", "error", err)
			}

			if ok, _ := common.Exists(filename); ok {
				if err := os.Remove(filename); err != nil {
					slog.Error("failed to remove socket", "error", err)
				}
			}
		})

		return "nbd+unix://?socket=" + filename, listener, nil
	} else {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return "", nil, fmt.Errorf("failed to listen: %v", err)
		}

		return "nbd://" + listener.Addr().String(), listener, nil
	}
}

type mountInfo struct {
	HostDirectory string
	Writable      bool
}

func (tr *TinyRange) runWithConfig() error {
	if len(tr.configs) == 0 {
		return fmt.Errorf("no configs specified")
	}

	topConfig := tr.configs[0]

	if topConfig.StorageSize == 0 || topConfig.CPUCores == 0 || topConfig.MemoryMB == 0 {
		return fmt.Errorf("invalid config")
	}

	if topConfig.Debug {
		slog.Warn("enabling hypervisor debug mode")
		tr.debug = true
	}

	// Catch Interrupt signals to shutdown gracefully.
	osSignal := make(chan os.Signal, 1)
	signal.Notify(osSignal, os.Interrupt)

	go func() {
		<-osSignal

		for _, fn := range tr.onExit {
			fn()
		}

		os.Exit(1)
	}()

	interaction := topConfig.Interaction
	if interaction == "" {
		interaction = "ssh"
	}

	start := time.Now()

	var exportedPorts []int
	var mountedHostDirectories []mountInfo

	root := filesystem.NewMemoryDirectory()

	for _, config := range tr.configs {
		for _, frag := range config.RootFsFragments {
			if port := frag.ExportPort; port != nil {
				exportedPorts = append(exportedPorts, port.Port)
			} else if mount := frag.MountHostDirectory; mount != nil {
				mountedHostDirectories = append(mountedHostDirectories, mountInfo{
					HostDirectory: config.Resolve(mount.HostDirectory),
					Writable:      mount.Writable,
				})
			} else {
				if err := tr.fragmentToFilesystem(config, frag, root); err != nil {
					return fmt.Errorf("failed to extract fragment to filesystem: %w", err)
				}
			}
		}
	}

	var secureSSH SecureSSHConfig

	// Configure secure SSH.
	if tr.secureSSH != "" {
		var err error

		secureSSH, err = tr.generateOrLoadSecureSSH()
		if err != nil {
			return fmt.Errorf("failed to generate or load secure ssh: %w", err)
		}

		secureConfig, err := json.Marshal(secureSSH)
		if err != nil {
			return fmt.Errorf("failed to marshal secure ssh config: %w", err)
		}

		memFile := filesystem.NewMemoryFile(filesystem.TypeRegular)

		if err := memFile.Overwrite(secureConfig); err != nil {
			return fmt.Errorf("failed to overwrite secure ssh config: %w", err)
		}

		if err := memFile.Chmod(0600); err != nil {
			return fmt.Errorf("failed to chmod secure ssh config: %w", err)
		}

		if _, err := filesystem.CreateChild(root, "/init.d/secure_ssh.json", memFile); err != nil {
			return fmt.Errorf("failed to create secure ssh config: %w", err)
		}
	} else {
		secureSSH.HostKey = ""
		secureSSH.PublicKey = ""
		secureSSH.Password = config.INSECURE_SSH_PASSWORD
	}

	slog.Debug("built filesystem tree", "took", time.Since(start))

	totalSize, err := filesystem.GetTotalSize(root)
	if err != nil {
		return fmt.Errorf("could not compute total size")
	}

	fsSize := int64(topConfig.StorageSize * 1024 * 1024)

	if int64(float64(totalSize)*1.5) > fsSize {
		targetSize := int64(float64(totalSize)*1.5) / 128 / 1024 / 1024

		slog.Debug("resize filesystem", "new", fmt.Sprintf("%dmb", targetSize*128))

		fsSize = targetSize * 128 * 1024 * 1024
	}

	start = time.Now()

	vmem := vm.NewVirtualMemory(fsSize, 4096)

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

	nbdAddress, listener, err := tr.createNbdListener(true)
	if err != nil {
		return fmt.Errorf("failed to create nbd listener: %w", err)
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

	if tr.wireguardUrl != "" {
		resp, err := tr.client.Get(tr.wireguardUrl)
		if err != nil {
			return fmt.Errorf("failed to get wireguard config: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("failed to get wireguard config from %s: %s", tr.wireguardUrl, resp.Status)
		}

		config, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("failed to read wireguard config: %w", err)
		}

		if err := ns.SetupWireguard(string(config), 1420); err != nil {
			return fmt.Errorf("failed to setup wireguard: %w", err)
		}
	}

	// out, err := os.Create("local/network.pcap")
	// if err != nil {
	// 	return err
	// }
	// defer out.Close()

	// ns.OpenPacketCapture(out)

	factory, err := virtualMachine.LoadVirtualMachineFactory(tr.buildDir, topConfig.Resolve(topConfig.HypervisorScript))
	if err != nil {
		return fmt.Errorf("failed to load virtual machine factory: %w", err)
	}

	virtualMachine, err := factory.Create(
		topConfig.CPUCores,
		topConfig.MemoryMB,
		topConfig.Architecture,
		topConfig.Resolve(topConfig.KernelFilename),
		topConfig.Resolve(topConfig.InitFilesystemFilename),
		nbdAddress,
		topConfig.Interaction,
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

	top := filesystem.NewMemoryDirectory()

	for _, dir := range mountedHostDirectories {
		name := filepath.Base(dir.HostDirectory)

		var hostDir filesystem.Directory

		if dir.Writable {
			hostDir = filesystem.NewLocalMutableDirectory(dir.HostDirectory)
		} else {
			hostDir = filesystem.NewLocalDirectory(dir.HostDirectory)
		}

		if _, err := filesystem.CreateChild(top, name, hostDir); err != nil {
			return fmt.Errorf("failed to create child: %w", err)
		}
	}

	if len(mountedHostDirectories) > 0 {
		slog.Info("host directories avalible via SFTP on sftp://host.internal")
	}

	svr := sftp.NewInternalServer(top, ":22")

	go func() {
		if err := svr.Run(func(network, addr string) (net.Listener, error) {
			slog.Debug("listening", "addr", addr)
			return ns.ListenInternal("tcp", addr)
		}); err != nil {
			slog.Error("failed to run sftp server", "err", err)
		}
	}()

	slog.Debug("starting virtual machine", "took", time.Since(start))

	tr.onExit = append(tr.onExit, func() {
		if err := virtualMachine.Shutdown(); err != nil {
			slog.Error("failed to shutdown virtual machine", "err", err)
		}
	})

	defer func() {
		for _, fn := range tr.onExit {
			fn()
		}
	}()

	if interaction == "ssh" || interaction == "vnc" {
		go func() {
			if err := virtualMachine.Run(nic, tr.debug); err != nil {
				slog.Error("failed to run virtual machine", "err", err)
				os.Exit(1)
			}
		}()

		// return nil

		if interaction == "vnc" {
			go runVncClient(ns, "10.42.0.2:5901")
		}

		// Start a loop so SSH can be restarted when requested by the user.
		for {
			err = connectOverSsh(ns, "10.42.0.2:2222", "root", secureSSH)
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

		return nil
	} else if strings.HasPrefix(interaction, "webssh") {
		go func() {
			if err := virtualMachine.Run(nic, tr.debug); err != nil {
				slog.Error("failed to run virtual machine", "err", err)
				os.Exit(1)
			}
		}()

		return runWebSsh(ns, "10.42.0.2:2222", "root", secureSSH, strings.TrimPrefix(interaction, "webssh,"))
	} else {
		return fmt.Errorf("unknown interaction: %s", interaction)
	}
}

func RunWithConfig(
	buildDir string,
	configs []config.TinyRangeConfig,
	debug bool,
	forwardSsh bool,
	exportFilesystem string,
	listenNbd string,
	streamingServer string,
	wireguardUrl string,
	secureSSH string,
	persistPath string,
) error {
	tr := &TinyRange{
		buildDir:         buildDir,
		configs:          configs,
		debug:            debug,
		forwardSsh:       forwardSsh,
		exportFilesystem: exportFilesystem,
		listenNbd:        listenNbd,
		streamingServer:  streamingServer,
		wireguardUrl:     wireguardUrl,
		client:           http.DefaultClient,
		secureSSH:        secureSSH,
		persistPath:      persistPath,
	}

	return tr.runWithConfig()
}

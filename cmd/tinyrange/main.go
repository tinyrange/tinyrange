package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	goFs "io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"github.com/miekg/dns"
	"github.com/tinyrange/tinyrange/pkg/archive"
	"github.com/tinyrange/tinyrange/pkg/builder"
	"github.com/tinyrange/tinyrange/pkg/buildinfo"
	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/config"
	"github.com/tinyrange/tinyrange/pkg/database"
	"github.com/tinyrange/tinyrange/pkg/filesystem/ext4"
	initExec "github.com/tinyrange/tinyrange/pkg/init"
	"github.com/tinyrange/tinyrange/pkg/netstack"
	_ "github.com/tinyrange/tinyrange/pkg/platform"
	virtualMachine "github.com/tinyrange/tinyrange/pkg/vm"
	gonbd "github.com/tinyrange/tinyrange/third_party/go-nbd"
	"github.com/tinyrange/vm"
	"gopkg.in/yaml.v3"
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

func runWithConfig(buildDir string, cfg config.TinyRangeConfig, debug bool, forwardSsh bool, exportFilesystem string) error {
	if cfg.StorageSize == 0 || cfg.CPUCores == 0 || cfg.MemoryMB == 0 {
		return fmt.Errorf("invalid config")
	}

	slog.Info("TinyRange", "version", buildinfo.VERSION)

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
			fh, err := os.Open(cfg.Resolve(ark.HostFilename))
			if err != nil {
				return fmt.Errorf("failed to open archive: %w", err)
			}

			entries, err := archive.ReadArchiveFromFile(fh)
			if err != nil {
				return fmt.Errorf("failed to read archive: %w", err)
			}

			for _, ent := range entries {
				name := ark.Target + "/" + ent.CName

				if fs.Exists(name) {
					continue
				}

				switch ent.CTypeflag {
				case archive.TypeDirectory:
					// slog.Info("directory", "name", name)
					name = strings.TrimSuffix(name, "/")
					if err := fs.Mkdir(name, true); err != nil {
						return fmt.Errorf("failed to mkdir in guest: %w", err)
					}
				case archive.TypeSymlink:
					// slog.Info("symlink", "name", name)
					if err := fs.Symlink(name, ent.CLinkname); err != nil {
						return fmt.Errorf("failed to symlink in guest: %w", err)
					}
				case archive.TypeLink:
					// slog.Info("link", "name", name)
					if err := fs.Link(name, "/"+ent.CLinkname); err != nil {
						return fmt.Errorf("failed to link in guest: %w", err)
					}
				case archive.TypeRegular:
					// slog.Info("reg", "name", name)
					f, err := ent.Open()
					if err != nil {
						return fmt.Errorf("failed to open file for guest: %w", err)
					}

					region, err := vm.NewFileRegion(f)
					if err != nil {
						return fmt.Errorf("failed to make region for guest: %w", err)
					}

					if err := fs.CreateFile(name, region); err != nil {
						return fmt.Errorf("failed to create file in guest %s: %w", name, err)
					}
				default:
					return fmt.Errorf("unimplemented entry type: %s", ent.CTypeflag)
				}

				if err := fs.Chown(name, uint16(ent.CUid), uint16(ent.CGid)); err != nil {
					return fmt.Errorf("failed to chown in guest: %w", err)
				}

				if err := fs.Chmod(name, goFs.FileMode(ent.CMode)); err != nil {
					return fmt.Errorf("failed to chmod in guest: %w", err)
				}
			}
		} else {
			return fmt.Errorf("unknown fragment kind")
		}
	}

	slog.Info("built filesystem", "took", time.Since(start))

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

	slog.Info("starting virtual machine", "took", time.Since(start))

	if interaction == "ssh" {
		go func() {
			if err := virtualMachine.Run(nic, debug); err != nil {
				slog.Error("failed to run virtual machine", "err", err)
			}
		}()
		defer virtualMachine.Shutdown()

		// return nil

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

func parseOciImageName(name string) *builder.FetchOciImageDefinition {
	name, tag, ok := strings.Cut(name, ":")

	if !ok {
		tag = "latest"
	}

	return builder.NewFetchOCIImageDefinition(
		builder.DEFAULT_REGISTRY,
		name,
		tag,
		"amd64",
	)
}

func runWithCommandLineConfig(buildDir string, rebuild bool, image string, execCommand string, cpuCores int, memoryMb int, storageSize int) error {
	db := database.New(buildDir)

	fragments := []config.Fragment{
		{Builtin: &config.BuiltinFragment{Name: "init", GuestFilename: "/init"}},
		{Builtin: &config.BuiltinFragment{Name: "init.star", GuestFilename: "/init.star"}},
	}

	{
		def := parseOciImageName(image)

		ctx := db.NewBuildContext(def)

		res, err := db.Build(ctx, def, common.BuildOptions{
			AlwaysRebuild: rebuild,
		})
		if err != nil {
			return err
		}

		if err := builder.ParseJsonFromFile(res, &def); err != nil {
			return err
		}

		for _, hash := range def.LayerArchives {
			filename, err := ctx.FilenameFromDigest(hash)
			if err != nil {
				return err
			}

			fragments = append(fragments, config.Fragment{Archive: &config.ArchiveFragment{HostFilename: filename}})
		}
	}

	if execCommand != "" {
		initJson, err := json.Marshal(&struct {
			SSHCommand []string `json:"ssh_command"`
		}{
			SSHCommand: []string{"/bin/sh", "-c", execCommand},
		})
		if err != nil {
			return err
		}

		fragments = append(fragments, config.Fragment{FileContents: &config.FileContentsFragment{GuestFilename: "/init.json", Contents: initJson}})
	}

	kernelFilename := ""

	{
		def := builder.NewFetchHttpBuildDefinition(builder.OFFICIAL_KERNEL_URL, 0)

		ctx := db.NewBuildContext(def)

		res, err := db.Build(ctx, def, common.BuildOptions{
			AlwaysRebuild: rebuild,
		})
		if err != nil {
			return err
		}

		kernelFilename, err = ctx.FilenameFromDigest(res.Digest())
		if err != nil {
			return err
		}
	}

	hypervisorScript, err := common.GetAdjacentExecutable("tinyrange_qemu.star")
	if err != nil {
		return err
	}

	return runWithConfig(buildDir, config.TinyRangeConfig{
		HypervisorScript: hypervisorScript,
		KernelFilename:   kernelFilename,
		CPUCores:         cpuCores,
		MemoryMB:         memoryMb,
		RootFsFragments:  fragments,
		StorageSize:      storageSize,
		Interaction:      "ssh",
	}, *debug, false, "")
}

var (
	cpuCores         = flag.Int("cpu-cores", 1, "set the number of cpu cores in the VM")
	memoryMb         = flag.Int("memory", 1024, "set the number of megabytes of RAM in the VM")
	storageSize      = flag.Int("storage-size", 512, "the size of the VM storage in megabytes")
	image            = flag.String("image", "library/alpine:latest", "the OCI image to boot inside the virtual machine")
	configFile       = flag.String("config", "", "passes a custom config. this overrides all other flags.")
	debug            = flag.Bool("debug", false, "redirect output from the hypervisor to the host. the guest will exit as soon as the VM finishes startup")
	buildDir         = flag.String("build-dir", common.GetDefaultBuildDir(), "the directory to build definitions to")
	rebuild          = flag.Bool("rebuild", false, "always rebuild the kernel and image definitions")
	execCommand      = flag.String("exec", "", "if set then run a command rather than creating a login shell")
	printVersion     = flag.Bool("version", false, "print the version information")
	exportFilesystem = flag.String("export-filesystem", "", "write the filesystem to the host filesystem")
)

func tinyRangeMain() error {
	flag.Parse()

	if *printVersion {
		fmt.Printf("TinyRange version: %s\nThe University of Queensland\n", buildinfo.VERSION)
		return nil
	}

	if err := common.Ensure(*buildDir, fs.ModePerm); err != nil {
		return fmt.Errorf("failed to create build dir: %w", err)
	}

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

		return runWithConfig(*buildDir, cfg, *debug, false, *exportFilesystem)
	} else {
		return runWithCommandLineConfig(*buildDir, *rebuild, *image, *execCommand, *cpuCores, *memoryMb, *storageSize)
	}
}

func main() {
	if err := tinyRangeMain(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

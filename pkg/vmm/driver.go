package vmm

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/csv"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/miekg/dns"
	"github.com/schollz/progressbar/v3"
	"github.com/tinyrange/tinyrange/pkg/archive"
	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/config"
	"github.com/tinyrange/tinyrange/pkg/feature"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/filesystem/ext4"
	"github.com/tinyrange/tinyrange/pkg/filesystem/p9"
	"github.com/tinyrange/tinyrange/pkg/filesystem/sftp"
	"github.com/tinyrange/tinyrange/pkg/filesystem/vm"
	"github.com/tinyrange/tinyrange/pkg/hash"
	initExec "github.com/tinyrange/tinyrange/pkg/init"
	"github.com/tinyrange/tinyrange/pkg/netstack"
	"github.com/tinyrange/tinyrange/pkg/path"
	_ "github.com/tinyrange/tinyrange/pkg/platform"
	"github.com/tinyrange/tinyrange/pkg/vmm/accelerate"
	gonbd "github.com/tinyrange/tinyrange/third_party/go-nbd"
	"github.com/tinyrange/tinyrange/third_party/go-nbd/backend"
	"golang.org/x/crypto/ssh"
	"gopkg.in/yaml.v3"
)

type Filesystem interface {
	// EnsurePath ensures the path exists.
	EnsurePath(path string) error

	// WriteFile writes a file to the filesystem.
	WriteFile(path string, contents []byte) error
}

type BlockDevice interface {
	io.ReaderAt
	io.WriterAt
	Size() int64
}

type fileBlockDevice struct {
	*os.File
	size int64
}

// // ReadAt implements BlockDevice.
// // Subtle: this method shadows the method (*File).ReadAt of fileBlockDevice.File.
// func (f *fileBlockDevice) ReadAt(p []byte, off int64) (n int, err error) {
// 	slog.Debug("reading", "len", len(p), "off", off)
// 	return f.File.ReadAt(p, off)
// }

// Size implements BlockDevice.
func (f *fileBlockDevice) Size() int64 {
	return f.size
}

// // WriteAt implements BlockDevice.
// // Subtle: this method shadows the method (*File).WriteAt of fileBlockDevice.File.
// func (f *fileBlockDevice) WriteAt(p []byte, off int64) (n int, err error) {
// 	n, err = f.File.WriteAt(p, off)
// 	slog.Debug("writing", "len", len(p), "off", off, "err", err)
// 	return
// }

var (
	_ BlockDevice = &fileBlockDevice{}
)

type vmBackend struct {
	vm BlockDevice
}

// Close implements common.Backend.
func (vm *vmBackend) Close() error {
	return nil
}

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

type PrepareResult struct {
}

type File interface {
	// GetNBDServer returns the NBD server address and the export name.
	// If unix is true, it returns a Unix domain socket path.
	// Otherwise, it returns a TCP address.
	GetNBDServer(unix bool) (net.Addr, string, error)

	// HostFilename writes the file to a temporary file and returns the path.
	HostFilename() (string, error)
}

type localFile struct {
	driver   *driver
	filename string
}

func (f *localFile) HostFilename() (string, error) {
	return f.filename, nil
}

func (f *localFile) GetNBDServer(unix bool) (net.Addr, string, error) {
	return nil, "", fmt.Errorf("not supported for local files")
}

var (
	_ File = &localFile{}
)

type NetworkInterface interface {
	// MACAddress returns the MAC address.
	MACAddress() net.HardwareAddr

	// GetUDPSocketPair returns a pair of UDP sockets one for sending L2 and one for receiving L2 packets.
	GetUDPSocketPair() (net.Addr, net.Addr, error)

	// AttachFile attaches a file to the network interface.
	AttachFile(file *os.File) error
}

type networkInterface struct {
	nic *netstack.NetworkInterface
}

func (ni *networkInterface) MACAddress() net.HardwareAddr {
	return ni.nic.MacAddress
}

func (ni *networkInterface) GetUDPSocketPair() (net.Addr, net.Addr, error) {
	return ni.nic.GetUDPSocketPair()
}

func (ni *networkInterface) AttachFile(file *os.File) error {
	return ni.nic.AttachFile(file)
}

var (
	_ NetworkInterface = &networkInterface{}
)

type VirtualMachineMonitor interface {
	// Run starts the virtual machine.
	// If bindOutput is true, the output is bound to the current process.
	Run(bindOutput bool) error

	// Shutdown stops the virtual machine.
	Shutdown() error
}

type executable struct {
	name string
	args []string

	mtx sync.Mutex
	cmd *exec.Cmd
}

func (exe *executable) Run(bindOutput bool) error {
	exe.mtx.Lock()

	slog.Debug("running hypervisor", "command", exe.name, "args", exe.args)

	exe.cmd = exec.Command(exe.name, exe.args...)

	if bindOutput {
		exe.cmd.Stdout = os.Stdout
		exe.cmd.Stderr = os.Stderr
		exe.cmd.Stdin = os.Stdin
	}

	exe.mtx.Unlock()

	if err := exe.cmd.Run(); err != nil {
		return fmt.Errorf("failed to run virtual machine: %s", err)
	}

	slog.Warn("virtual machine exited")

	return nil
}

func (exe *executable) Shutdown() error {
	exe.mtx.Lock()
	defer exe.mtx.Unlock()

	if exe.cmd != nil {
		return exe.cmd.Process.Kill()
	}
	return nil
}

func NewExecutable(name string, args []string) VirtualMachineMonitor {
	return &executable{
		name: name,
		args: args,
	}
}

type Driver interface {
	// FindExecutable finds the executable with the given name.
	// It looks beside the current executable first then searches the PATH.
	FindExecutable(name string) (string, error)

	// HostOperatingSystem returns the host operating system.
	HostOperatingSystem() string

	// GuestArchitecture returns the guest architecture. This is the architecture of the kernel and init.
	GuestArchitecture() config.CPUArchitecture

	// RootArchitecture returns the architecture of executables on the guest filesystem so emulation can be installed.
	RootArchitecture() config.CPUArchitecture

	// Accelerated returns true if the driver supports hardware acceleration.
	Accelerated() bool

	// CPUCount returns the number of CPU cores.
	CPUCores() int

	// MemorySize returns the memory size in MiB.
	MemoryMB() int

	// DiskImages returns a list of attached disk images.
	DiskImages() []File

	// InitRamFs returns the initial ramdisk filesystem or nil.
	InitRamFs() File

	// Verbose returns true if the driver is in verbose mode.
	Verbose() bool

	// Experimental returns the experimental flags.
	Experimental() []string

	// Interaction returns the interaction mode.
	Interaction() config.InteractionKind

	// NetworkInterface returns the network interface or nil.
	NetworkInterface() NetworkInterface

	// Kernel returns the kernel image.
	Kernel() File

	// EnsureFile ensures the file with the given name exists and has the given contents.
	EnsureFile(contents []byte) (File, error)
}

var startTime = time.Now()

type loggerRegion struct {
	vm.MemoryRegion
	filename string
	write    func([]string) error
}

func (r *loggerRegion) ReadAt(p []byte, off int64) (n int, err error) {
	n, err = r.MemoryRegion.ReadAt(p, off)
	if err != nil {
		return
	}

	r.write([]string{
		fmt.Sprintf("%f", time.Since(startTime).Seconds()),
		r.filename,
		fmt.Sprintf("%d", n),
		fmt.Sprintf("%d", off),
	})

	return
}

type driver struct {
	configs           []config.TinyRangeConfig
	buildDir          string
	debug             bool
	secureSSH         string
	persistPath       string
	exportFsPath      string
	dumpFsPath        string
	nbdBlockSize      int
	wireguardUrl      string
	packetCapturePath string

	dumpWriter *csv.Writer

	kernel           File
	initRamFs        File
	diskImages       []File
	networkInterface NetworkInterface

	onExit       []func()
	deletedFiles map[string]bool
}

func (tr *driver) fragmentToFilesystem(cfg config.TinyRangeConfig, frag config.Fragment, dir filesystem.MutableDirectory) error {
	if localFile := frag.LocalFile; localFile != nil {
		file := filesystem.NewLocalFile(cfg.Resolve(localFile.HostFilename), nil)

		overlay, err := filesystem.NewOverlayFile(file)
		if err != nil {
			return err
		}

		if localFile.Executable {
			if err := overlay.Chmod(fs.FileMode(0755)); err != nil {
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
			if err := file.Chmod(fs.FileMode(0755)); err != nil {
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

			if err := file.Chmod(fs.FileMode(0755)); err != nil {
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
		} else if builtin.Name == "tinyrange_qemu" {
			local, err := common.GetAdjacentExecutable("tinyrange_qemu", "tinyqemu/tinyrange_qemu")
			if err != nil {
				return fmt.Errorf("failed to get tinyrange_qemu: %w", err)
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
			fsark filesystem.Archive
			err   error
		)

		// if tr.streamingServer != "" {
		// 	f := filesystem.NewRemoteFile(tr.client, tr.streamingServer+ark.HostFilename)

		// 	archive, err = filesystem.ReadArchiveFromStreamingServer(tr.client, tr.streamingServer, f)
		// 	if err != nil {
		// 		return fmt.Errorf("failed to download archive: %w", err)
		// 	}
		// } else {
		f := filesystem.NewLocalFile(cfg.Resolve(ark.HostFilename), nil)

		fsark, err = archive.ReadArchiveFromFile(f)
		if err != nil {
			return fmt.Errorf("failed to read archive: %w", err)
		}
		// }

		entries, err := fsark.Entries()
		if err != nil {
			return fmt.Errorf("failed to read archive: %w", err)
		}

		for _, ent := range entries {
			// TODO(joshua): Why is this not path.Native.Join?
			name := ark.Target + "/" + ent.Name()

			if _, ok := tr.deletedFiles[name]; ok {
				continue
			}

			var file filesystem.MutableFile

			if name != "/" {
				if filesystem.Exists(dir, name) {
					continue
				}

				dirname := path.Unix.Dir(name)

				if !filesystem.Exists(dir, dirname) && path.Unix.Clean(name) != dirname {
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
				case filesystem.TypeDeleted:
					if err := filesystem.DeleteChild(dir, name); err != nil {
						return err
					}

					tr.deletedFiles[name] = true

					continue
				default:
					return fmt.Errorf("unimplemented entry type: %s", ent.Typeflag())
				}
			} else {
				file = dir
			}

			if err := file.Chown(ent.Uid(), ent.Gid()); err != nil {
				return fmt.Errorf("failed to chown in guest: %w", err)
			}

			if err := file.Chmod(fs.FileMode(ent.Mode())); err != nil {
				return fmt.Errorf("failed to chmod in guest: %w", err)
			}
		}

		return nil
	} else {
		return fmt.Errorf("unknown fragment kind: %+v", frag)
	}
}

func (tr *driver) generateOrLoadSecureSSH() (SecureSSHConfig, error) {
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

type nbdAddress struct {
	Addr   net.Addr
	Export string
}

func (nbd *nbdAddress) GetNBDServer(unix bool) (net.Addr, string, error) {
	if unix && nbd.Addr.Network() == "unix" {
		return nbd.Addr, nbd.Export, nil
	} else if !unix && nbd.Addr.Network() == "tcp" {
		return nbd.Addr, nbd.Export, nil
	} else {
		return nil, "", fmt.Errorf("invalid address: %+v %s %s", unix, nbd.Addr.Network(), nbd.Addr.String())
	}
}

func (nbd *nbdAddress) HostFilename() (string, error) {
	return "", fmt.Errorf("not supported for nbd")
}

var (
	_ File = &nbdAddress{}
)

type ext4Filesystem struct {
	*nbdAddress

	fs *ext4.Ext4Filesystem
}

func (fs *ext4Filesystem) EnsurePath(path string) error {
	return fs.fs.Mkdir(path, true)
}

func (fs *ext4Filesystem) WriteFile(path string, contents []byte) error {
	return fs.fs.CreateFile(path, vm.RawRegion(contents))
}

var (
	_ Filesystem = &ext4Filesystem{}
)

func (tr *driver) createNbdListener(tryUnix bool) (net.Addr, net.Listener, error) {
	if (runtime.GOOS == "linux" || runtime.GOOS == "darwin" || runtime.GOOS == "windows") && tr.persistPath != "" && tryUnix {
		pid := os.Getpid()

		filename := path.Native.Join(tr.persistPath, fmt.Sprintf("%d.nbd.sock", pid))

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

		return listener.Addr(), listener, nil
	} else {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return nil, nil, fmt.Errorf("failed to listen: %v", err)
		}

		return listener.Addr(), listener, nil
	}
}

func (tr *driver) fragmentsToConfig(name string) (filesystem.Directory, []int, []mountInfo, []volumeInfo, error) {
	var exportedPorts []int
	var mountedHostDirectories []mountInfo
	var volumes []volumeInfo

	root := filesystem.NewMemoryDirectory()

	tr.deletedFiles = make(map[string]bool)

	for _, config := range tr.configs {
		fsInfo, ok := config.Filesystems[name]
		if !ok {
			continue
		}

		for _, frag := range fsInfo.Fragments {
			if port := frag.ExportPort; port != nil {
				exportedPorts = append(exportedPorts, port.Port)
			} else if mount := frag.MountHostDirectory; mount != nil {
				mountedHostDirectories = append(mountedHostDirectories, mountInfo{
					HostDirectory: config.Resolve(mount.HostDirectory),
					Port:          mount.Port,
					Writable:      mount.Writable,
				})
			} else if volume := frag.AddVolume; volume != nil {
				volumes = append(volumes, volumeInfo{
					VolumeName:    volume.VolumeName,
					MinimumSizeMB: volume.MinimumSizeMB,
					GuestPath:     volume.GuestPath,
					Persist:       volume.Persist,
				})
			} else {
				if err := tr.fragmentToFilesystem(config, frag, root); err != nil {
					return nil, nil, nil, nil, fmt.Errorf("failed to extract fragment to filesystem: %w", err)
				}
			}
		}
	}

	return root, exportedPorts, mountedHostDirectories, volumes, nil
}

func (tr *driver) configureSecureSSH(root filesystem.Directory) (SecureSSHConfig, error) {
	var secureSSH SecureSSHConfig

	// Configure secure SSH.
	if tr.secureSSH != "" {
		var err error

		secureSSH, err = tr.generateOrLoadSecureSSH()
		if err != nil {
			return SecureSSHConfig{}, fmt.Errorf("failed to generate or load secure ssh: %w", err)
		}

		secureConfig, err := json.Marshal(secureSSH)
		if err != nil {
			return SecureSSHConfig{}, fmt.Errorf("failed to marshal secure ssh config: %w", err)
		}

		memFile := filesystem.NewMemoryFile(filesystem.TypeRegular)

		if err := memFile.Overwrite(secureConfig); err != nil {
			return SecureSSHConfig{}, fmt.Errorf("failed to overwrite secure ssh config: %w", err)
		}

		if err := memFile.Chmod(0600); err != nil {
			return SecureSSHConfig{}, fmt.Errorf("failed to chmod secure ssh config: %w", err)
		}

		if _, err := filesystem.CreateChild(root, "/init.d/secure_ssh.json", memFile); err != nil {
			return SecureSSHConfig{}, fmt.Errorf("failed to create secure ssh config: %w", err)
		}
	} else {
		secureSSH.HostKey = ""
		secureSSH.PublicKey = ""
		secureSSH.Password = config.INSECURE_SSH_PASSWORD
	}

	return secureSSH, nil
}

func (tr *driver) buildFilesystem(
	kind config.FilesystemKind,
	minSize int,
	name string,
	persistPath string,
	root filesystem.Directory,
	skipDirectories map[filesystem.Directory]struct{},
) (
	bd BlockDevice,
	ext4Fs *ext4.Ext4Filesystem,
	fsSize int64,
	err error,
) {
	totalSize, err := filesystem.GetTotalSize(root)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("could not compute total size")
	}

	if int64(float64(totalSize)*1.5) > int64(minSize)*1024*1024 {
		targetSize := int64(float64(totalSize)*1.5) / 128 / 1024 / 1024

		slog.Debug("resize filesystem", "new", fmt.Sprintf("%dmb", targetSize*128))

		fsSize = targetSize * 128 * 1024 * 1024
	} else {
		fsSize = int64(minSize) * 1024 * 1024
	}

	start := time.Now()

	vmem := vm.NewVirtualMemory(fsSize, 4096)
	bd = vmem

	slog.Debug("created virtual memory", "took", time.Since(start))

	switch kind {
	case config.FilesystemKindExt4:
		start = time.Now()

		fs, err := ext4.CreateExt4Filesystem(vmem, 0, fsSize)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("failed to create ext4 filesystem: %w", err)
		}
		ext4Fs = fs

		slog.Debug("created ext4 filesystem", "took", time.Since(start))

		start = time.Now()

		var regionWrapper ext4.RegionWrapperFunc

		if tr.dumpWriter != nil {
			regionWrapper = func(filename string, region vm.MemoryRegion) vm.MemoryRegion {
				return &loggerRegion{
					MemoryRegion: region,
					filename:     filename,
					write:        tr.dumpWriter.Write,
				}
			}
		}

		if err := fs.AddDirectory(root, regionWrapper, skipDirectories); err != nil {
			return nil, nil, 0, fmt.Errorf("failed to add directory to filesystem: %w", err)
		}

		slog.Debug("built filesystem", "took", time.Since(start))
	case config.FilesystemKindRaw:
		// noop
	default:
		return nil, nil, 0, fmt.Errorf("unknown filesystem kind: %s", kind)
	}

	if persistPath != "" {
		if tr.persistPath == "" {
			return nil, nil, 0, fmt.Errorf("persist path not set for persistent filesystem")
		}

		persistDir := path.Native.Dir(persistPath)
		persistName := name + "_" + path.Native.Base(persistPath)

		persistPath := path.Native.Join(tr.persistPath, path.Native.Join(persistDir, persistName))

		fh, err := os.OpenFile(persistPath, os.O_RDWR, 0644)
		if errors.Is(err, os.ErrNotExist) {
			slog.Info("creating persistent filesystem", "size", fsSize, "path", persistPath)

			fh, err = os.Create(persistPath)
			if err != nil {
				return nil, nil, 0, fmt.Errorf("failed to create persistent filesystem: %w", err)
			}

			if err := fh.Truncate(fsSize); err != nil {
				return nil, nil, 0, fmt.Errorf("failed to truncate persistent filesystem: %w", err)
			}

			if _, err := io.Copy(io.NewOffsetWriter(fh, 0), io.NewSectionReader(vmem, 0, fsSize)); err != nil {
				return nil, nil, 0, fmt.Errorf("failed to copy memory to persistent filesystem: %w", err)
			}
		} else if err == nil {
			slog.Info("opened persistent filesystem", "size", fsSize, "path", persistPath)

			info, err := fh.Stat()
			if err != nil {
				return nil, nil, 0, fmt.Errorf("failed to stat persistent filesystem: %w", err)
			}

			if info.Size() != fsSize {
				return nil, nil, 0, fmt.Errorf("persistent filesystem size mismatch: %d != %d", info.Size(), fsSize)
			}
		} else {
			return nil, nil, 0, fmt.Errorf("failed to open persistent filesystem: %w", err)
		}

		return &fileBlockDevice{File: fh, size: fsSize}, nil, fsSize, nil
	} else {
		return
	}
}

type nbdExport struct {
	name    string
	backend backend.Backend
}

func (tr *driver) nbdLoop(listener net.Listener, exports ...nbdExport) {
	for {
		conn, err := listener.Accept()
		if errors.Is(err, net.ErrClosed) {
			return
		} else if err != nil {
			slog.Error("nbd server failed to accept", "error", err)
			return
		}

		go func(conn net.Conn) {
			minBlockSize := uint32(512)
			preferredBlockSize := uint32(1024)
			maximumBlockSize := uint32(32*1024*1024 - 1)
			if tr.nbdBlockSize != 0 {
				minBlockSize = min(512, uint32(tr.nbdBlockSize))
				preferredBlockSize = min(4096, uint32(tr.nbdBlockSize))
				maximumBlockSize = min(32*1024*1024-1, uint32(tr.nbdBlockSize))
			}

			exportList := make([]gonbd.Export, 0, len(exports))
			for _, export := range exports {
				exportList = append(exportList, gonbd.Export{
					Name:        export.name,
					Description: "",
					Backend:     export.backend,
				})
			}

			// slog.Debug("got nbd connection", "remote", conn.RemoteAddr().String())
			err = gonbd.Handle(conn, exportList, &gonbd.Options{
				ReadOnly:           false,
				MinimumBlockSize:   minBlockSize, // Fix for VZ on Darwin, it errors if the minimum is too large.
				PreferredBlockSize: preferredBlockSize,
				MaximumBlockSize:   maximumBlockSize,
			})
			if err != nil {
				slog.Warn("nbd server failed to handle", "error", err)
			}
		}(conn)
	}
}

func (tr *driver) startDNSServer(ns *netstack.NetStack) error {
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

	return nil
}

func (tr *driver) exportPort(ns *netstack.NetStack, port int) error {
	slog.Info("exporting port", "address", fmt.Sprintf("localhost:%d", port))

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

	return nil
}

func (d *driver) topConfig() *config.TinyRangeConfig {
	return &d.configs[0]
}

type mountInfo struct {
	HostDirectory string
	Port          int
	Writable      bool
}

type volumeInfo struct {
	VolumeName    string
	MinimumSizeMB uint64
	GuestPath     string
	Persist       bool
}

func (d *driver) exec(create func(vmm Driver) (VirtualMachineMonitor, error)) error {
	mainStart := time.Now()

	topConfig := d.topConfig()

	if topConfig.CPUCores == 0 || topConfig.MemoryMB == 0 {
		return fmt.Errorf("invalid config")
	}

	if topConfig.Debug {
		slog.Warn("enabling hypervisor debug mode")
		d.debug = true
	}

	// Catch Interrupt signals to shutdown gracefully.
	osSignal := make(chan os.Signal, 1)
	signal.Notify(osSignal, os.Interrupt)

	go func() {
		<-osSignal

		for _, fn := range d.onExit {
			fn()
		}

		os.Exit(1)
	}()

	if topConfig.Interaction == "" {
		topConfig.Interaction = config.InteractionSSH
	}

	start := time.Now()

	root, exportedPorts, mountedHostDirectories, volumes, err := d.fragmentsToConfig("root")
	if err != nil {
		return fmt.Errorf("failed to convert fragments to config: %w", err)
	}

	slog.Debug("built filesystem tree", "took", time.Since(start))

	secureSSH, err := d.configureSecureSSH(root)
	if err != nil {
		return fmt.Errorf("failed to configure secure ssh: %w", err)
	}

	if d.dumpFsPath != "" {
		dumpFile, err := os.Create(d.dumpFsPath)
		if err != nil {
			return fmt.Errorf("failed to create dump file: %w", err)
		}
		defer dumpFile.Close()

		d.dumpWriter = csv.NewWriter(dumpFile)
		defer d.dumpWriter.Flush()

		d.dumpWriter.Write([]string{"time", "filename", "size", "offset"})
	}

	rootInfo, ok := topConfig.Filesystems["root"]
	if !ok {
		return fmt.Errorf("root filesystem not found")
	}

	if d.exportFsPath != "" {
		vmem, _, fsSize, err := d.buildFilesystem(
			rootInfo.Kind,
			rootInfo.StorageSize,
			"root",
			"",
			root,
			make(map[filesystem.Directory]struct{}),
		)
		if err != nil {
			return fmt.Errorf("failed to build filesystem: %w", err)
		}

		out, err := os.Create(d.exportFsPath)
		if err != nil {
			return fmt.Errorf("failed to create export filesystem: %w", err)
		}
		defer out.Close()

		pb := progressbar.DefaultBytes(fsSize, "exporting filesystem")
		defer pb.Close()

		if _, err := io.Copy(io.MultiWriter(pb, out), io.NewSectionReader(vmem, 0, fsSize)); err != nil {
			return fmt.Errorf("failed to copy export filesystem: %w", err)
		}

		slog.Debug("exported filesystem", "took", time.Since(start))

		return nil
	}

	rootKind := rootInfo.Kind

	addr, listener, err := d.createNbdListener(true)
	if err != nil {
		return fmt.Errorf("failed to create nbd listener: %w", err)
	}

	var exports []nbdExport
	skipDirectories := make(map[filesystem.Directory]struct{})

	var rootFilesystem Filesystem

	for _, volume := range volumes {
		if _, err := filesystem.Mkdir(root, volume.GuestPath); err != nil {
			return fmt.Errorf("failed to open path: %w", err)
		}
	}

	for _, volume := range append([]volumeInfo{
		{VolumeName: "root", GuestPath: "/", MinimumSizeMB: uint64(rootInfo.StorageSize)},
	}, volumes...) {
		rootDir, err := filesystem.Mkdir(root, volume.GuestPath)
		if err != nil {
			return fmt.Errorf("failed to open path: %w", err)
		}

		persist := rootInfo.PersistPath
		if volume.Persist {
			persist = "persist.img"
		}

		vmem, ext4Fs, _, err := d.buildFilesystem(
			rootKind,
			int(volume.MinimumSizeMB),
			volume.VolumeName,
			persist,
			rootDir,
			skipDirectories,
		)
		if err != nil {
			return fmt.Errorf("failed to build filesystem: %w", err)
		}

		nbdAddress := &nbdAddress{Addr: addr, Export: volume.VolumeName}

		exports = append(exports, nbdExport{
			name:    volume.VolumeName,
			backend: &vmBackend{vm: vmem},
		})

		if ext4Fs != nil {
			disk := &ext4Filesystem{
				nbdAddress: nbdAddress,
				fs:         ext4Fs,
			}

			d.diskImages = append(d.diskImages, disk)

			if volume.VolumeName == "root" {
				rootFilesystem = disk
			}
		} else {
			d.diskImages = append(d.diskImages, nbdAddress)
		}

		skipDirectories[rootDir] = struct{}{}
	}

	if rootFilesystem != nil && len(volumes) > 0 {
		if true { // guest os Linux
			if err := rootFilesystem.EnsurePath("/init.d"); err != nil {
				return fmt.Errorf("failed to ensure path: %w", err)
			}

			mountNames := []string{"vdb", "vdc", "vdd", "vde", "vdf", "vdg"}

			mountScript := "def main():\n"
			for i, volume := range volumes {
				mountScript += fmt.Sprintf(
					"  mount(\"ext4\", \"/dev/%s\", \"%s\", ensure_path = True)\n",
					mountNames[i], volume.GuestPath,
				)
			}

			if err := rootFilesystem.WriteFile("/init.d/mount.star", []byte(mountScript)); err != nil {
				return fmt.Errorf("failed to write file: %w", err)
			}
		}
	}

	go d.nbdLoop(listener, exports...)

	ns := netstack.New()

	if d.wireguardUrl != "" {
		resp, err := http.Get(d.wireguardUrl)
		if err != nil {
			return fmt.Errorf("failed to get wireguard config: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("failed to get wireguard config from %s: %s", d.wireguardUrl, resp.Status)
		}

		config, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("failed to read wireguard config: %w", err)
		}

		if err := ns.SetupWireguard(string(config), 1420); err != nil {
			return fmt.Errorf("failed to setup wireguard: %w", err)
		}
	}

	if d.packetCapturePath != "" {
		writer, err := os.Create(d.packetCapturePath)
		if err != nil {
			return fmt.Errorf("failed to create packet capture: %w", err)
		}
		defer writer.Close()

		if err := ns.OpenPacketCapture(writer); err != nil {
			return fmt.Errorf("failed to open packet capture: %w", err)
		}
	}

	nic, err := ns.AttachNetworkInterface()
	if err != nil {
		return fmt.Errorf("failed to attach network interface: %w", err)
	}

	d.networkInterface = &networkInterface{
		nic: nic,
	}

	// Create DNS server.
	if err := d.startDNSServer(ns); err != nil {
		return fmt.Errorf("failed to start DNS server: %w", err)
	}

	// Export ports.
	for _, port := range exportedPorts {
		if err := d.exportPort(ns, port); err != nil {
			return fmt.Errorf("failed to export port: %w", err)
		}
	}

	// Set the kernel.
	if topConfig.KernelFilename != "" {
		d.kernel = &localFile{
			driver:   d,
			filename: topConfig.Resolve(topConfig.KernelFilename),
		}
	}
	if topConfig.InitFilesystemFilename != "" {
		d.initRamFs = &localFile{
			driver:   d,
			filename: topConfig.Resolve(topConfig.InitFilesystemFilename),
		}
	}

	// Create the virtual machine monitor.
	vmm, err := create(d)
	if err != nil {
		return fmt.Errorf("failed to create virtual machine monitor: %w", err)
	}

	if feature.HasFeature(feature.Feature9P) {
		for _, dir := range mountedHostDirectories {
			var hostDir filesystem.Directory

			if dir.Writable {
				hostDir = filesystem.NewLocalMutableDirectory(dir.HostDirectory)
			} else {
				hostDir = filesystem.NewLocalDirectory(dir.HostDirectory)
			}

			svr := p9.NewServer(hostDir)

			listen, err := ns.ListenInternal("tcp", fmt.Sprintf(":%d", dir.Port))
			if err != nil {
				return fmt.Errorf("failed to listen internal (9p): %w", err)
			}

			go func() {
				if err := svr.Serve(listen); err != nil {
					slog.Error("failed to run 9p server", "err", err)
				}
			}()
		}
	} else {
		top := filesystem.NewMemoryDirectory()

		for _, dir := range mountedHostDirectories {
			name := path.Native.Base(dir.HostDirectory)

			var hostDir filesystem.Directory

			if dir.Writable {
				hostDir = filesystem.NewLocalMutableDirectory(dir.HostDirectory)
			} else {
				hostDir = filesystem.NewLocalDirectory(dir.HostDirectory)
			}

			if _, err := filesystem.CreateChild(top, name, hostDir); err != nil {
				return fmt.Errorf("failed to create child %s: %w", name, err)
			}
		}

		if len(mountedHostDirectories) > 0 {
			slog.Info("host directories avalible via SFTP on sftp://host.internal")
		}

		svr := sftp.NewInternalServer(top, ":22")

		go func() {
			if err := svr.Run(func(network, addr string) (net.Listener, error) {
				return ns.ListenInternal("tcp", addr)
			}); err != nil {
				slog.Error("failed to run sftp server", "err", err)
			}
		}()
	}

	slog.Debug("starting virtual machine", "took", time.Since(start))

	d.onExit = append(d.onExit, func() {
		if err := vmm.Shutdown(); err != nil {
			slog.Error("failed to shutdown virtual machine", "err", err)
		}
	})

	defer func() {
		for _, fn := range d.onExit {
			fn()
		}
	}()

	slog.Debug("running virtual machine", "initTime", time.Since(mainStart))

	switch d.Interaction() {
	case config.InteractionSSH, config.InteractionVNC:
		go func() {
			if err := vmm.Run(d.debug); err != nil {
				slog.Error("failed to run virtual machine", "err", err)
				os.Exit(1)
			}
		}()

		if d.Interaction() == config.InteractionVNC {
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
	case config.InteractionSerial:
		if err := vmm.Run(true); err != nil {
			return fmt.Errorf("failed to run virtual machine: %w", err)
		}

		return nil
	case config.InteractionWebSSH, config.InteractionWebSSHMinimal, config.InteractionWebSSHNoBrower:
		go func() {
			if err := vmm.Run(d.debug); err != nil {
				slog.Error("failed to run virtual machine", "err", err)
				os.Exit(1)
			}
		}()

		return runWebSsh(ns, "10.42.0.2:2222", "root", secureSSH, strings.TrimPrefix(string(d.Interaction()), "webssh,"))
	default:
		return fmt.Errorf("unsupported interaction mode: %s", d.Interaction())
	}
}

func (d *driver) addConfig(p string) error {
	var cfg config.TinyRangeConfig

	f, err := os.Open(p)
	if err != nil {
		return fmt.Errorf("failed to open config file: %w", err)
	}

	if path.Native.Ext(p) == ".json" {
		if err := json.NewDecoder(f).Decode(&cfg); err != nil {
			return fmt.Errorf("failed to decode config file: %w", err)
		}
	} else if path.Native.Ext(p) == ".yaml" || path.Native.Ext(p) == ".yml" {
		if err := yaml.NewDecoder(f).Decode(&cfg); err != nil {
			return fmt.Errorf("failed to decode config file: %w", err)
		}
	} else {
		return fmt.Errorf("unknown file extension: %s", path.Native.Ext(p))
	}

	d.configs = append(d.configs, cfg)

	return nil
}

func (d *driver) FindExecutable(name string) (string, error) {
	myPath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("failed to get executable path: %w", err)
	}

	// If the name exists then return it.
	if _, err := os.Stat(name); err == nil {
		return name, nil
	}

	// Look in the same directory as the current executable.
	dir := path.Native.Dir(myPath)
	if _, err := os.Stat(path.Native.Join(dir, name)); err == nil {
		return path.Native.Join(dir, name), nil
	}

	// Look in the PATH.
	filename, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("failed to find executable: %w", err)
	}

	return filename, nil
}

func (d *driver) Accelerated() bool {
	if !d.GuestArchitecture().IsNative() {
		return false
	}

	return accelerate.SupportsAcceleration()
}

func (d *driver) HostOperatingSystem() string               { return runtime.GOOS }
func (d *driver) GuestArchitecture() config.CPUArchitecture { return d.topConfig().Architecture }
func (d *driver) CPUCores() int                             { return d.topConfig().CPUCores }
func (d *driver) MemoryMB() int                             { return d.topConfig().MemoryMB }
func (d *driver) DiskImages() []File                        { return d.diskImages }
func (d *driver) InitRamFs() File                           { return d.initRamFs }
func (d *driver) Verbose() bool                             { return common.IsVerbose() }
func (d *driver) Experimental() []string                    { return feature.GetFeatureFlags() }
func (d *driver) Interaction() config.InteractionKind       { return d.topConfig().Interaction }
func (d *driver) NetworkInterface() NetworkInterface        { return d.networkInterface }
func (d *driver) Kernel() File                              { return d.kernel }

func (d *driver) RootArchitecture() config.CPUArchitecture {
	return d.topConfig().RootArchitecture
}

func (d *driver) EnsureFile(contents []byte) (File, error) {
	if len(contents) == 0 {
		return nil, fmt.Errorf("empty contents")
	}

	hash := hash.GetSha256Hash(contents)

	path := path.Native.Join(d.buildDir, string(hash)+".bin")

	slog.Debug("ensure file", "path", path, "length", len(contents))

	if ok, _ := common.Exists(path); !ok {
		if err := os.WriteFile(path, contents, os.ModePerm); err != nil {
			return &localFile{driver: d, filename: path}, err
		}
	}

	return &localFile{driver: d, filename: path}, nil
}

var (
	_ Driver = &driver{}
)

var (
	doPrepare         = flag.Bool("prepare", false, "prepare the driver and check if it is runnable")
	buildDir          = flag.String("build-dir", common.GetDefaultBuildDir(), "the build directory")
	debug             = flag.Bool("debug", false, "enable debug mode")
	verbose           = flag.Bool("verbose", false, "enable verbose mode")
	secureSSH         = flag.String("secure-ssh", "", "Specify a local file to save a secure SSH config to. This will set a random persistent host key and root password.")
	persistPath       = flag.String("persist-path", "", "Specify a path to save VM files to.")
	exportFsPath      = flag.String("exportfs", "", "Export the filesystem to a file.")
	dumpFsPath        = flag.String("dumpfs", "", "Dump the filename and offset of any reads from the filesystem to a CSV file.")
	wireguardUrl      = flag.String("wireguard-url", "", "URL to fetch wireguard config from.")
	nbdBlockSize      = flag.Int("nbd-block-size", 0, "Override the preferred and maximum block size for the NBD server. This can have major performance implications.")
	packetCapturePath = flag.String("packet-capture", "", "Path to write packet capture in pcap format to.")
)

func entryMain(
	prepare func(vmm Driver) (PrepareResult, error),
	create func(vmm Driver) (VirtualMachineMonitor, error),
) error {
	flag.Parse()

	if *verbose {
		common.EnableVerbose()
	}

	driver := &driver{
		buildDir:          *buildDir,
		debug:             *debug,
		secureSSH:         *secureSSH,
		persistPath:       *persistPath,
		exportFsPath:      *exportFsPath,
		dumpFsPath:        *dumpFsPath,
		wireguardUrl:      *wireguardUrl,
		nbdBlockSize:      *nbdBlockSize,
		packetCapturePath: *packetCapturePath,
	}

	if *doPrepare {
		out, err := prepare(driver)
		if err != nil {
			slog.Error("fatal", "err", err)
			os.Exit(1)
		}

		enc, err := json.Marshal(out)
		if err != nil {
			return err
		}

		if _, err := os.Stdout.Write(enc); err != nil {
			return err
		}
	}

	for _, arg := range flag.Args() {
		if err := driver.addConfig(arg); err != nil {
			return err
		}
	}

	if len(driver.configs) == 0 {
		return fmt.Errorf("no configs provided")
	}

	if err := driver.exec(create); err != nil {
		return err
	}

	return nil
}

func Entry(
	prepare func(vmm Driver) (PrepareResult, error),
	create func(vmm Driver) (VirtualMachineMonitor, error),
) {
	if os.Getenv("TINYRANGE_VERBOSE") == "on" {
		if err := common.EnableVerbose(); err != nil {
			slog.Error("failed to enable verbose logging", "err", err)
			os.Exit(1)
		}
	}

	if err := entryMain(prepare, create); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

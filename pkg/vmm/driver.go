package vmm

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
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
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	goDebug "runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
	"github.com/things-go/go-socks5"
	"golang.org/x/crypto/ssh"
	"gopkg.in/yaml.v3"

	"github.com/tinyrange/tinyrange/pkg/archive"
	"github.com/tinyrange/tinyrange/pkg/archive2"
	"github.com/tinyrange/tinyrange/pkg/build2"
	"github.com/tinyrange/tinyrange/pkg/builder"
	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/config"
	"github.com/tinyrange/tinyrange/pkg/feature"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/filesystem/ext4"
	"github.com/tinyrange/tinyrange/pkg/filesystem/fsutil"
	"github.com/tinyrange/tinyrange/pkg/filesystem/p9"
	"github.com/tinyrange/tinyrange/pkg/filesystem/sftp"
	"github.com/tinyrange/tinyrange/pkg/filesystem/vm"
	"github.com/tinyrange/tinyrange/pkg/hash"
	initExec "github.com/tinyrange/tinyrange/pkg/init"
	"github.com/tinyrange/tinyrange/pkg/linux/goboot"
	"github.com/tinyrange/tinyrange/pkg/log"
	"github.com/tinyrange/tinyrange/pkg/netstack"
	"github.com/tinyrange/tinyrange/pkg/netstack/ns"
	"github.com/tinyrange/tinyrange/pkg/path"
	_ "github.com/tinyrange/tinyrange/pkg/platform"
	"github.com/tinyrange/tinyrange/pkg/trdf"
	"github.com/tinyrange/tinyrange/pkg/vmm/accelerate"
	gonbd "github.com/tinyrange/tinyrange/third_party/go-nbd"
	"github.com/tinyrange/tinyrange/third_party/go-nbd/backend"
	"github.com/tinyrange/tinyrange/third_party/memory"
)

type Filesystem interface {
	// EnsurePath ensures the path exists.
	EnsurePath(path string) error

	// WriteFile writes a file to the filesystem.
	WriteFile(path string, contents []byte) error
}

type BlockDevice interface {
	vm.MemoryRegion
	io.Closer
}

type fileBlockDevice struct {
	*os.File
	size int64
}

// Size implements BlockDevice.
func (f *fileBlockDevice) Size() int64 {
	return f.size
}

var (
	_ BlockDevice = &fileBlockDevice{}
)

type diskBlockDevice struct {
	*trdf.Disk
}

var (
	_ BlockDevice = &diskBlockDevice{}
)

type virtualBlockDevice struct {
	*vm.VirtualMemory
}

// Close implements BlockDevice.
func (v *virtualBlockDevice) Close() error {
	return nil
}

var (
	_ BlockDevice = &virtualBlockDevice{}
)

type vmBackend struct {
	vm     BlockDevice
	driver *driver
}

// Close implements common.Backend.
func (vm *vmBackend) Close() error {
	return nil
}

// ReadAt implements common.Backend.
func (vm *vmBackend) ReadAt(p []byte, off int64) (n int, err error) {
	n, err = vm.vm.ReadAt(p, off)
	if err != nil {
		vm.driver.log.Error("vmBackend readAt", "len", len(p), "off", off, "err", err)

		// assume the VM will detect this as corruption and exit immediately.
		vm.driver.fatalError()

		return 0, nil
	}

	return
}

// WriteAt implements common.Backend.
func (vm *vmBackend) WriteAt(p []byte, off int64) (n int, err error) {
	n, err = vm.vm.WriteAt(p, off)
	if err != nil {
		vm.driver.log.Error("vmBackend writeAt", "len", len(p), "off", off, "err", err)

		// assume the VM will detect this as corruption and exit immediately.
		vm.driver.fatalError()

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
	Run(log log.Handler, bindOutput bool) error

	// Shutdown stops the virtual machine.
	Shutdown() error
}

type ProxyMonitor interface {
	VirtualMachineMonitor

	NetStack() ns.NetStack
}

type executable struct {
	name string
	args []string

	mtx sync.Mutex
	cmd *exec.Cmd
}

func (exe *executable) Run(log log.Handler, bindOutput bool) error {
	exe.mtx.Lock()

	log.Debug("running hypervisor", "command", exe.name, "args", exe.args)

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

	log.Warn("virtual machine exited")

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

type ProxyDriver interface {
	// URL returns the URL of the driver. The URL can contain parameters for the driver.
	URL() *url.URL

	// Configs returns the list of configs for the driver.
	Configs() []config.TinyRangeConfig

	// BuildDatabase returns the build database.
	BuildDatabase() build2.BuildCacheFilesystem

	// Logger returns the logger for the driver.
	Logger() log.Handler
}

type Driver interface {
	ProxyDriver

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
	log log.Handler

	configs                    []config.TinyRangeConfig
	configFilenames            []string
	buildDir                   string
	debug                      bool
	secureSSH                  string
	persistPath                string
	exportFsPath               string
	dumpFsPath                 string
	nbdBlockSize               int
	wireguardUrl               string
	packetCapturePath          string
	cpuCores                   int
	memoryMB                   int
	socks5Listener             net.Listener
	socks5Proxy                string
	noValidateTinyRangeVersion bool
	driverUrl                  *url.URL

	dbBuildDir build2.BuildCacheFilesystem

	dumpWriter *csv.Writer

	ns ns.NetStack

	kernel           File
	initRamFs        File
	diskImages       []File
	networkInterface NetworkInterface

	onExit       []func()
	deletedFiles map[string]bool

	simpleCache map[string][]byte

	// Convenience flags
	name             string
	sshKey           string
	sshListenAddress string
	sshPort          int
}

// Logger implements Driver.
func (tr *driver) Logger() log.Handler {
	return tr.log
}

func (tr *driver) fatalError() {
	for _, f := range tr.onExit {
		f()
	}

	os.Exit(1)
}

func (tr *driver) getOrCreateDefaultBuildDirectory() (string, error) {
	// look upwards for a tinyrange.portable file.
	// if it exists, use that as the build directory.
	var currentDir string

	executable, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("failed to get executable: %w", err)
	}

	currentDir = path.Native.Dir(executable)
	for {
		if _, err := os.Stat(path.Native.Join(currentDir, "tinyrange.portable")); err == nil {
			// found the tinyrange.portable file, use this directory as the build directory.

			if err := os.MkdirAll(path.Native.Join(currentDir, "build"), 0755); err != nil {
				return "", fmt.Errorf("failed to create build directory: %w", err)
			}

			tr.log.Info("found tinyrange.portable, using build directory", "dir", path.Native.Join(currentDir, "build"))

			return path.Native.Join(currentDir, "build"), nil
		}

		if path.Native.Dir(currentDir) == currentDir {
			// reached the root directory, stop searching.
			break
		}
		currentDir = path.Native.Dir(currentDir)
	}

	// otherwise use the default build directory.
	defaultDir := common.GetDefaultBuildDir()

	tr.log.Info("using default build directory", "dir", defaultDir)

	if err := os.MkdirAll(defaultDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create build directory: %w", err)
	}

	return defaultDir, nil
}

func (tr *driver) loadBuildDatabase() error {
	var err error

	topConfig := tr.topConfig()

	if len(topConfig.BuildDatabaseConfig) == 0 {
		return fmt.Errorf("no build database config")
	}

	topConfigFilename := tr.configFilenames[0]

	dbConfig := topConfig.BuildDatabaseConfig[0]

	if dbConfig.DefaultBuildDirectory != nil {
		buildDir, err := tr.getOrCreateDefaultBuildDirectory()
		if err != nil {
			return fmt.Errorf("failed to get default build directory: %w", err)
		}

		mutBuildDir := filesystem.Factory.NewLocalMutableDirectory(buildDir)

		// set the build directory to the default build directory.
		if tr.buildDir == common.GetDefaultBuildDir() {
			tr.buildDir = buildDir
		}

		tr.dbBuildDir, err = build2.OpenFilesystemBuildCache(mutBuildDir, build2.DEFAULT_DATABASE_CONFIG, tr.log)
		if err != nil {
			return fmt.Errorf("failed to open build database: %w", err)
		}
	} else if dbConfig.RelativeHostBuildDirectory != nil {
		if topConfigFilename == "" || topConfigFilename == "<stdin>" {
			return fmt.Errorf("no build database config filename")
		}

		buildDir := path.Native.Join(path.Native.Dir(topConfigFilename), dbConfig.RelativeHostBuildDirectory.RelativePath)

		if ok, _ := common.Exists(buildDir); !ok {
			return fmt.Errorf("build directory does not exist: %s", buildDir)
		}

		mutBuildDir := filesystem.Factory.NewLocalMutableDirectory(buildDir)

		tr.dbBuildDir, err = build2.OpenFilesystemBuildCache(mutBuildDir, build2.DEFAULT_DATABASE_CONFIG, tr.log)
		if err != nil {
			return fmt.Errorf("failed to open build database: %w", err)
		}
	} else {
		return fmt.Errorf("top config build database config is not a relative host build directory")
	}

	for _, cfg := range topConfig.BuildDatabaseConfig[1:] {
		if cfg.RelativeHostBuildDirectory != nil {
			return fmt.Errorf("relative host build directory not supported for additional build database config")
		} else if cfg.AbsoluteHostBuildDirectory != nil {
			buildDir := cfg.AbsoluteHostBuildDirectory.AbsolutePath

			if ok, _ := common.Exists(buildDir); !ok {
				return fmt.Errorf("build directory does not exist: %s", buildDir)
			}

			readOnlyBuildDir := filesystem.Factory.NewLocalDirectory(buildDir)

			if err := tr.dbBuildDir.AddCacheDirectory(readOnlyBuildDir, cfg); err != nil {
				return fmt.Errorf("failed to add build directory: %w", err)
			}
		} else if cfg.Archive2BuildArtifact != nil {
			hashDir, err := tr.dbBuildDir.GetBuildDirectory(hash.Hash(cfg.Archive2BuildArtifact.Hash))
			if err != nil {
				return fmt.Errorf("failed to get build directory: %w", err)
			}

			ark, err := builder.Archive2FromArtifact(hashDir)
			if err != nil {
				return fmt.Errorf("failed to get archive2 from artifact: %w", err)
			}
			defer ark.Close()

			dir := filesystem.Factory.NewMemoryDirectory()

			if err := fsutil.ExtractArchive2ToFilesystem(ark, tr, "", dir, nil); err != nil {
				return fmt.Errorf("failed to extract archive: %w", err)
			}

			if cfg.Archive2BuildArtifact.Path != "" {
				pathEnt, err := fsutil.OpenPath(dir, cfg.Archive2BuildArtifact.Path)
				if err != nil {
					return fmt.Errorf("failed to open path in archive: %w", err)
				}

				dir, ok := pathEnt.File.(filesystem.Directory)
				if !ok {
					return fmt.Errorf("path in archive is not a directory: %s", cfg.Archive2BuildArtifact.Path)
				}

				if err := tr.dbBuildDir.AddCacheDirectory(dir, cfg); err != nil {
					return fmt.Errorf("failed to add build directory: %w", err)
				}
			} else {
				if err := tr.dbBuildDir.AddCacheDirectory(dir, cfg); err != nil {
					return fmt.Errorf("failed to add build directory: %w", err)
				}
			}
		} else if cfg.RemoteBuildDirectory != nil {
			if err := tr.dbBuildDir.AddRemoteCacheDirectory(cfg.RemoteBuildDirectory.BaseURL, cfg); err != nil {
				return fmt.Errorf("failed to add remote build directory: %w", err)
			}
		} else {
			return fmt.Errorf("unknown build database config: %v", cfg)
		}
	}

	return nil
}

func (tr *driver) ResolveReference(ref config.DatabaseReference) (filesystem.File, error) {
	if tr.dbBuildDir == nil {
		return nil, fmt.Errorf("no build filesystem set")
	}

	return tr.dbBuildDir.FileFromReference(ref)
}

// GetOrSetCacheForHash implements filesystem.ExtendedRegionMethods.
func (tr *driver) GetOrSetCacheForHash(hash string, setter func(w io.Writer) error) (io.ReaderAt, error) {
	if tr.persistPath == "" {
		if tr.simpleCache == nil {
			tr.simpleCache = make(map[string][]byte)
		}

		if _, ok := tr.simpleCache[hash]; !ok {
			var buf bytes.Buffer

			if err := setter(&buf); err != nil {
				return nil, err
			}

			tr.simpleCache[hash] = buf.Bytes()
		}

		return bytes.NewReader(tr.simpleCache[hash]), nil
	} else {
		if err := os.MkdirAll(path.Native.Join(tr.persistPath, "runtimeCache"), 0755); err != nil {
			return nil, fmt.Errorf("failed to create cache directory: %w", err)
		}

		filename := path.Native.Join(tr.persistPath, "runtimeCache", hash)

		if ok, _ := common.Exists(filename); !ok {
			fh, err := os.Create(filename)
			if err != nil {
				return nil, fmt.Errorf("failed to create file: %w", err)
			}

			if err := setter(fh); err != nil {
				fh.Close()
				return nil, fmt.Errorf("failed to set cache: %w", err)
			}

			if err := fh.Close(); err != nil {
				return nil, fmt.Errorf("failed to close file: %w", err)
			}
		}

		fh, err := os.Open(filename)
		if err != nil {
			return nil, fmt.Errorf("failed to open file: %w", err)
		}

		return fh, nil
	}
}

// HttpClient implements filesystem.ExtendedRegionMethods.
func (tr *driver) HttpClient() (*http.Client, error) {
	return http.DefaultClient, nil
}

func (tr *driver) fragmentToFilesystem(frag config.Fragment, dir filesystem.MutableDirectory) error {
	if localFile := frag.LocalFile; localFile != nil {
		// The local file is a path to a file on the host. It is guaranteed to be absolute.
		file := filesystem.Factory.NewLocalFile(localFile.HostFilename, nil)

		overlay, err := filesystem.Factory.NewOverlayFile(file)
		if err != nil {
			return err
		}

		if localFile.Executable {
			if err := overlay.Chmod(fs.FileMode(0755)); err != nil {
				return err
			}
		}

		if _, err := fsutil.CreateChild(dir, localFile.GuestFilename, overlay); err != nil {
			return err
		}

		return nil
	} else if localDir := frag.LocalDirectory; localDir != nil {
		// The local directory is a path to a directory on the host. It is guaranteed to be absolute.
		local := filesystem.Factory.NewLocalDirectory(localDir.HostDirectory)

		if _, err := fsutil.CreateChild(dir, localDir.GuestDirectory, local); err != nil {
			return err
		}

		return nil
	} else if databaseFile := frag.DatabaseFile; databaseFile != nil {
		file, err := tr.ResolveReference(databaseFile.DatabaseReference)
		if err != nil {
			return fmt.Errorf("failed to resolve host filename: %w", err)
		}

		overlay, err := filesystem.Factory.NewOverlayFile(file)
		if err != nil {
			return err
		}

		if databaseFile.Executable {
			if err := overlay.Chmod(fs.FileMode(0755)); err != nil {
				return err
			}
		}

		if _, err := fsutil.CreateChild(dir, databaseFile.GuestFilename, overlay); err != nil {
			return err
		}

		return nil
	} else if fileContents := frag.FileContents; fileContents != nil {
		file := filesystem.Factory.NewMemoryFile()

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

		if _, err := fsutil.CreateChild(dir, fileContents.GuestFilename, file); err != nil {
			return err
		}

		return nil
	} else if builtin := frag.Builtin; builtin != nil {
		switch builtin.Name {
		case "init":
			exec, err := initExec.GetInitExecutable(builtin.Architecture)
			if err != nil {
				return err
			}

			file := filesystem.Factory.NewMemoryFile()

			if err := file.Overwrite(exec); err != nil {
				return err
			}

			if err := file.Chmod(fs.FileMode(0755)); err != nil {
				return err
			}

			if _, err := fsutil.CreateChild(dir, builtin.GuestFilename, file); err != nil {
				return err
			}

			return nil
		case "tinyrange":
			exe, err := os.Executable()
			if err != nil {
				return fmt.Errorf("failed to get executable: %w", err)
			}

			file := filesystem.Factory.NewLocalFile(exe, nil)

			if _, err := fsutil.CreateChild(dir, builtin.GuestFilename, file); err != nil {
				return err
			}

			return nil
		case "tinyrange_qemu":
			local, err := common.GetAdjacentExecutable("tinyrange_qemu", "tinyqemu/tinyrange_qemu")
			if err != nil {
				return fmt.Errorf("failed to get tinyrange_qemu: %w", err)
			}

			file := filesystem.Factory.NewLocalFile(local, nil)

			if _, err := fsutil.CreateChild(dir, builtin.GuestFilename, file); err != nil {
				return err
			}

			return nil
		default:
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
		archiveFile, err := tr.ResolveReference(ark.DatabaseReference)
		if err != nil {
			return fmt.Errorf("failed to resolve host filename: %w", err)
		}

		fsark, err = archive.ReadArchiveFromFile(archiveFile)
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
				if fsutil.Exists(dir, name) {
					continue
				}

				dirname := path.Unix.Dir(name)

				if !fsutil.Exists(dir, dirname) && path.Unix.Clean(name) != dirname {
					// log.Info("mkdir", "dirname", dirname)
					if _, err := fsutil.Mkdir(dir, dirname); err != nil {
						return err
					}
				}

				switch ent.Typeflag() {
				case filesystem.TypeDirectory:
					// log.Info("directory", "name", name)
					name = strings.TrimSuffix(name, "/")

					file, err = fsutil.Mkdir(dir, name)
					if err != nil {
						return err
					}
				case filesystem.TypeSymlink:
					// log.Info("symlink", "name", name)
					symlink := filesystem.Factory.NewSymlink(ent.Linkname())

					file = symlink

					if _, err := fsutil.CreateChild(dir, name, symlink); err != nil {
						return err
					}
				case filesystem.TypeLink:
					// log.Info("link", "name", name, "target", ent.Linkname())
					link, err := filesystem.Factory.NewHardLink(ent.Linkname())
					if err != nil {
						return err
					}

					file = link

					if _, err := fsutil.CreateChild(dir, name, link); err != nil {
						return err
					}
				case filesystem.TypeRegular:
					// log.Info("reg", "name", name)
					file, err = filesystem.Factory.NewOverlayFile(ent)
					if err != nil {
						return err
					}

					if _, err := fsutil.CreateChild(dir, name, ent); err != nil {
						return err
					}
				case filesystem.TypeDeleted:
					if err := fsutil.DeleteChild(dir, name); err != nil {
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

			if err := file.Chtimes(ent.ModTime()); err != nil {
				return fmt.Errorf("failed to set modtime in guest: %w", err)
			}
		}

		return nil
	} else if ark2 := frag.Archive2; ark2 != nil {
		index, err := tr.ResolveReference(ark2.IndexReference)
		if err != nil {
			return fmt.Errorf("failed to resolve index host filename: %w", err)
		}
		contents, err := tr.ResolveReference(ark2.ContentsReference)
		if err != nil {
			return fmt.Errorf("failed to resolve contents host filename: %w", err)
		}

		indexFh, err := index.Open()
		if err != nil {
			return fmt.Errorf("failed to open index: %w", err)
		}

		contentsFh, err := contents.Open()
		if err != nil {
			return fmt.Errorf("failed to open contents: %w", err)
		}
		// don't close contents

		ark, err := archive2.NewArchiveReader(indexFh, indexFh, contentsFh)
		if err != nil {
			return fmt.Errorf("failed to read archive: %w", err)
		}
		defer ark.Close()

		if err := fsutil.ExtractArchive2ToFilesystem(ark, tr, ark2.Target, dir, tr.deletedFiles); err != nil {
			return fmt.Errorf("failed to extract archive: %w", err)
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

		// Upgrade older files missing keys.
		needsWrite := false
		if secureSSH.HostKey == "" {
			hostKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			if err != nil {
				return SecureSSHConfig{}, fmt.Errorf("ssh: failed to generate server host key: %v", err)
			}
			hostBlock, err := ssh.MarshalPrivateKey(hostKey, "")
			if err != nil {
				return SecureSSHConfig{}, fmt.Errorf("ssh: failed to marshal server host key: %v", err)
			}
			hostPem := pem.EncodeToMemory(hostBlock)
			if hostPem == nil {
				return SecureSSHConfig{}, fmt.Errorf("ssh: failed to encode server host key")
			}
			secureSSH.HostKey = string(hostPem)

			hostPub, err := ssh.NewPublicKey(&hostKey.PublicKey)
			if err != nil {
				return SecureSSHConfig{}, fmt.Errorf("ssh: failed to generate server public key: %v", err)
			}
			secureSSH.PublicKey = string(ssh.MarshalAuthorizedKey(hostPub))
			needsWrite = true
		}
		if secureSSH.ClientPrivateKey == "" || secureSSH.AuthorizedKey == "" {
			_, clientPrivRaw, err := ed25519.GenerateKey(rand.Reader)
			if err != nil {
				return SecureSSHConfig{}, fmt.Errorf("ssh: failed to generate client key: %v", err)
			}
			clientBlock, err := ssh.MarshalPrivateKey(clientPrivRaw, "")
			if err != nil {
				return SecureSSHConfig{}, fmt.Errorf("ssh: failed to marshal client private key: %v", err)
			}
			clientPem := pem.EncodeToMemory(clientBlock)
			if clientPem == nil {
				return SecureSSHConfig{}, fmt.Errorf("ssh: failed to encode client private key")
			}
			secureSSH.ClientPrivateKey = string(clientPem)
			clientPub, err := ssh.NewPublicKey(clientPrivRaw.Public())
			if err != nil {
				return SecureSSHConfig{}, fmt.Errorf("ssh: failed to derive client public key: %v", err)
			}
			secureSSH.AuthorizedKey = string(ssh.MarshalAuthorizedKey(clientPub))
			needsWrite = true
		}
		if needsWrite {
			// overwrite existing file
			_ = f.Close()
			wf, err := os.Create(tr.secureSSH)
			if err != nil {
				return SecureSSHConfig{}, fmt.Errorf("failed to update secure ssh config: %w", err)
			}
			defer wf.Close()
			if err := json.NewEncoder(wf).Encode(secureSSH); err != nil {
				return SecureSSHConfig{}, fmt.Errorf("failed to encode secure ssh config: %w", err)
			}
		}
	} else {
		// Generate a new server host key (ECDSA P-256).
		hostKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return SecureSSHConfig{}, fmt.Errorf("ssh: failed to generate server host key: %v", err)
		}

		hostBlock, err := ssh.MarshalPrivateKey(hostKey, "")
		if err != nil {
			return SecureSSHConfig{}, fmt.Errorf("ssh: failed to marshal server host key: %v", err)
		}
		hostPem := pem.EncodeToMemory(hostBlock)
		if hostPem == nil {
			return SecureSSHConfig{}, fmt.Errorf("ssh: failed to encode server host key")
		}
		secureSSH.HostKey = string(hostPem)

		hostPub, err := ssh.NewPublicKey(&hostKey.PublicKey)
		if err != nil {
			return SecureSSHConfig{}, fmt.Errorf("ssh: failed to generate server public key: %v", err)
		}
		secureSSH.PublicKey = string(ssh.MarshalAuthorizedKey(hostPub))

		// Generate a client keypair (ed25519 preferred if available via ssh.MarshalPrivateKey).
		// Use ed25519 from the standard library for key material; ssh will handle marshaling.
		_, clientPrivRaw, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return SecureSSHConfig{}, fmt.Errorf("ssh: failed to generate client key: %v", err)
		}
		clientBlock, err := ssh.MarshalPrivateKey(clientPrivRaw, "")
		if err != nil {
			return SecureSSHConfig{}, fmt.Errorf("ssh: failed to marshal client private key: %v", err)
		}
		clientPem := pem.EncodeToMemory(clientBlock)
		if clientPem == nil {
			return SecureSSHConfig{}, fmt.Errorf("ssh: failed to encode client private key")
		}
		secureSSH.ClientPrivateKey = string(clientPem)

		clientPub, err := ssh.NewPublicKey(clientPrivRaw.Public())
		if err != nil {
			return SecureSSHConfig{}, fmt.Errorf("ssh: failed to derive client public key: %v", err)
		}
		secureSSH.AuthorizedKey = string(ssh.MarshalAuthorizedKey(clientPub))

		// Write the config to disk for persistence.
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
			tr.log.Warn("failed to listen on unix socket", "error", err)
			return tr.createNbdListener(false)
		}

		tr.onExit = append(tr.onExit, func() {
			if err := listener.Close(); err != nil {
				tr.log.Error("failed to close listener", "error", err)
			}

			if ok, _ := common.Exists(filename); ok {
				if err := os.Remove(filename); err != nil {
					tr.log.Error("failed to remove socket", "error", err)
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

type portInformation struct {
	listenAddress string
	port          int
	guestPort     int
}

func (tr *driver) fragmentsToConfig(name string) (filesystem.Directory, []portInformation, []mountInfo, []volumeInfo, error) {
	var exportedPorts []portInformation
	var mountedHostDirectories []mountInfo
	var volumes []volumeInfo

	root := filesystem.Factory.NewMemoryDirectory()

	tr.deletedFiles = make(map[string]bool)

	for _, config := range tr.configs {
		fsInfo, ok := config.Filesystems[name]
		if !ok {
			continue
		}

		for _, frag := range fsInfo.Fragments {
			if port := frag.ExportPort; port != nil {
				exportedPorts = append(exportedPorts, portInformation{
					listenAddress: port.ListenAddress,
					port:          port.Port,
					guestPort:     port.Port,
				})
			} else if mount := frag.MountHostDirectory; mount != nil {
				mountedHostDirectories = append(mountedHostDirectories, mountInfo{
					HostDirectory: mount.HostDirectory, // the host directory is guaranteed to be absolute
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
				if err := tr.fragmentToFilesystem(frag, root); err != nil {
					return nil, nil, nil, nil, fmt.Errorf("failed to extract fragment to filesystem: %w", err)
				}
			}
		}
	}

	return root, exportedPorts, mountedHostDirectories, volumes, nil
}

func (tr *driver) configureSecureSSH(root filesystem.Directory) (SecureSSHConfig, error) {
	var secureSSH SecureSSHConfig

	// Helper to write a filtered guest JSON (no client private key).
	type guestSSHArgs struct {
		HostKey       string `json:"ssh_host_key"`
		AuthorizedKey string `json:"ssh_authorized_key"`
		Password      string `json:"ssh_password,omitempty"`
	}

	writeGuest := func(args guestSSHArgs) error {
		secureConfig, err := json.Marshal(args)
		if err != nil {
			return fmt.Errorf("failed to marshal guest ssh config: %w", err)
		}

		memFile := filesystem.Factory.NewMemoryFile()
		if err := memFile.Overwrite(secureConfig); err != nil {
			return fmt.Errorf("failed to overwrite secure ssh config: %w", err)
		}
		if err := memFile.Chmod(0600); err != nil {
			return fmt.Errorf("failed to chmod secure ssh config: %w", err)
		}
		// Overwrite if the file already exists.
		if fsutil.Exists(root, "/init.d/secure_ssh.json") {
			if err := fsutil.DeleteChild(root, "/init.d/secure_ssh.json"); err != nil {
				return fmt.Errorf("failed to delete existing secure ssh config: %w", err)
			}
		}
		if _, err := fsutil.CreateChild(root, "/init.d/secure_ssh.json", memFile); err != nil {
			return fmt.Errorf("failed to create secure ssh config: %w", err)
		}
		return nil
	}

	// Configure secure SSH.
	if tr.secureSSH != "" {
		var err error
		secureSSH, err = tr.generateOrLoadSecureSSH()
		if err != nil {
			return SecureSSHConfig{}, fmt.Errorf("failed to generate or load secure ssh: %w", err)
		}
	} else {
		// Generate ephemeral server host key and client keypair for this run only.
		hostKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return SecureSSHConfig{}, fmt.Errorf("ssh: failed to generate server host key: %v", err)
		}
		hostBlock, err := ssh.MarshalPrivateKey(hostKey, "")
		if err != nil {
			return SecureSSHConfig{}, fmt.Errorf("ssh: failed to marshal server host key: %v", err)
		}
		hostPem := pem.EncodeToMemory(hostBlock)
		if hostPem == nil {
			return SecureSSHConfig{}, fmt.Errorf("ssh: failed to encode server host key")
		}
		secureSSH.HostKey = string(hostPem)

		hostPub, err := ssh.NewPublicKey(&hostKey.PublicKey)
		if err != nil {
			return SecureSSHConfig{}, fmt.Errorf("ssh: failed to generate server public key: %v", err)
		}
		secureSSH.PublicKey = string(ssh.MarshalAuthorizedKey(hostPub))

		_, clientPrivRaw, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return SecureSSHConfig{}, fmt.Errorf("ssh: failed to generate client key: %v", err)
		}
		clientBlock, err := ssh.MarshalPrivateKey(clientPrivRaw, "")
		if err != nil {
			return SecureSSHConfig{}, fmt.Errorf("ssh: failed to marshal client private key: %v", err)
		}
		clientPem := pem.EncodeToMemory(clientBlock)
		if clientPem == nil {
			return SecureSSHConfig{}, fmt.Errorf("ssh: failed to encode client private key")
		}
		secureSSH.ClientPrivateKey = string(clientPem)
		clientPub, err := ssh.NewPublicKey(clientPrivRaw.Public())
		if err != nil {
			return SecureSSHConfig{}, fmt.Errorf("ssh: failed to derive client public key: %v", err)
		}
		secureSSH.AuthorizedKey = string(ssh.MarshalAuthorizedKey(clientPub))
	}

	// If a user-provided SSH identity is specified, authorize it on the guest and prefer it for client auth.
	if tr.sshKey != "" {
		keyBytes, err := os.ReadFile(tr.sshKey)
		if err != nil {
			return SecureSSHConfig{}, fmt.Errorf("failed to read --ssh-key: %w", err)
		}

		keyStr := strings.TrimSpace(string(keyBytes))

		// Try private key first.
		var privSigner ssh.Signer
		if signer, err := ssh.ParsePrivateKey([]byte(keyStr)); err == nil {
			privSigner = signer
		} else {
			// If it's a .pub, parse authorized key.
			if pub, _, _, _, perr := ssh.ParseAuthorizedKey([]byte(keyStr)); perr == nil {
				// Try to find matching private key by stripping .pub
				if strings.HasSuffix(tr.sshKey, ".pub") {
					if privBytes, rerr := os.ReadFile(strings.TrimSuffix(tr.sshKey, ".pub")); rerr == nil {
						if signer2, perr2 := ssh.ParsePrivateKey(privBytes); perr2 == nil {
							privSigner = signer2
							// Use the original private key content for client auth.
							secureSSH.ClientPrivateKey = string(privBytes)
						}
					}
				}
				// Use provided public key line.
				secureSSH.AuthorizedKey = string(ssh.MarshalAuthorizedKey(pub))
			} else {
				return SecureSSHConfig{}, fmt.Errorf("--ssh-key is neither a valid private key nor a valid public key")
			}
		}

		if privSigner != nil {
			// Set client private key for internal SSH client.
			// If the user provided a private key file (not .pub), override any previously generated key.
			if !strings.HasSuffix(tr.sshKey, ".pub") {
				secureSSH.ClientPrivateKey = keyStr
			}
			// Derive authorized_key from the private key's public component to ensure match.
			pub := privSigner.PublicKey()
			secureSSH.AuthorizedKey = string(ssh.MarshalAuthorizedKey(pub))
		}

		// Write updated guest args with the user-authorized key.
		if err := writeGuest(guestSSHArgs{
			HostKey:       secureSSH.HostKey,
			AuthorizedKey: secureSSH.AuthorizedKey,
			Password:      secureSSH.Password,
		}); err != nil {
			return SecureSSHConfig{}, err
		}

		// If persisting, update the persisted JSON but never store the user's private key.
		if tr.secureSSH != "" {
			persisted := secureSSH
			persisted.ClientPrivateKey = ""
			f, err := os.Create(tr.secureSSH)
			if err != nil {
				return SecureSSHConfig{}, fmt.Errorf("failed to update secure ssh config: %w", err)
			}
			if err := json.NewEncoder(f).Encode(persisted); err != nil {
				_ = f.Close()
				return SecureSSHConfig{}, fmt.Errorf("failed to encode secure ssh config: %w", err)
			}
			_ = f.Close()
		}
	} else {
		if err := writeGuest(guestSSHArgs{
			HostKey:       secureSSH.HostKey,
			AuthorizedKey: secureSSH.AuthorizedKey,
			Password:      secureSSH.Password,
		}); err != nil {
			return SecureSSHConfig{}, err
		}
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
	BlockDevice,
	*ext4.Ext4Filesystem,
	int64,
	error,
) {
	// start by computing the size of the filesystem.
	totalSize, err := fsutil.GetTotalSize(root)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("could not compute total size")
	}

	var fsSize int64
	if int64(float64(totalSize)*1.5) > int64(minSize)*1024*1024 {
		targetSize := int64(float64(totalSize)*1.5) / 128 / 1024 / 1024

		tr.log.Debug("resize filesystem", "new", fmt.Sprintf("%dmb", targetSize*128))

		fsSize = targetSize * 128 * 1024 * 1024
	} else {
		fsSize = int64(minSize) * 1024 * 1024
	}

	var (
		final  BlockDevice = nil
		create             = true
	)

	if persistPath != "" {
		if tr.persistPath == "" {
			return nil, nil, 0, fmt.Errorf("persist path not set for persistent filesystem")
		}

		persistDir := path.Native.Dir(persistPath)
		persistName := name + "_" + path.Native.Base(persistPath)

		persistPath := path.Native.Join(tr.persistPath, path.Native.Join(persistDir, persistName))

		if feature.HasFeature(feature.FeatureNewDiskFormat) {
			fh, err := os.OpenFile(persistPath, os.O_RDWR, 0644)
			if errors.Is(err, os.ErrNotExist) {
				fh, err = os.Create(persistPath)
				if err != nil {
					return nil, nil, 0, fmt.Errorf("failed to create persistent disk: %w", err)
				}
			} else if err != nil {
				return nil, nil, 0, fmt.Errorf("failed to open persistent disk: %w", err)
			} else {
				// the file already exists.
				create = false
			}

			disk, err := trdf.OpenDisk(fh, fsSize)
			if err != nil {
				return nil, nil, 0, fmt.Errorf("failed to open persistent disk: %w", err)
			}

			final = &diskBlockDevice{Disk: disk}
		} else {
			fh, err := os.OpenFile(persistPath, os.O_RDWR, 0644)
			if errors.Is(err, os.ErrNotExist) {
				fh, err = os.Create(persistPath)
				if err != nil {
					return nil, nil, 0, fmt.Errorf("failed to create persistent filesystem: %w", err)
				}

				if err := fh.Truncate(fsSize); err != nil {
					return nil, nil, 0, fmt.Errorf("failed to truncate persistent filesystem: %w", err)
				}
			} else if err == nil {
				info, err := fh.Stat()
				if err != nil {
					return nil, nil, 0, fmt.Errorf("failed to stat persistent filesystem: %w", err)
				}

				if info.Size() == fsSize {
					// all good
				} else if info.Size() < fsSize && feature.HasFeature(feature.FeatureExt4Resize) {
					tr.log.Warn("resizing persistent filesystem", "size", fsSize, "current", info.Size())
					if err := fh.Truncate(fsSize); err != nil {
						return nil, nil, 0, fmt.Errorf("failed to truncate persistent filesystem: %w", err)
					}
				} else {
					return nil, nil, 0, fmt.Errorf("persistent filesystem size mismatch: %d != %d", info.Size(), fsSize)
				}

				create = false
			} else {
				return nil, nil, 0, fmt.Errorf("failed to open persistent filesystem: %w", err)
			}

			final = &fileBlockDevice{File: fh, size: fsSize}
		}
	}

	// Only create the filesystem if it needs to be created.
	if create {
		if final != nil {
			tr.log.Info("creating persistent disk", "size", fsSize, "path", persistPath)
		}

		start := time.Now()

		vmem := vm.NewVirtualMemory(fsSize, 4096)
		bd := &virtualBlockDevice{VirtualMemory: vmem}

		tr.log.Debug("created virtual memory", "took", time.Since(start))

		var ext4Fs *ext4.Ext4Filesystem
		switch kind {
		case config.FilesystemKindExt4:
			start = time.Now()

			fs, err := ext4.CreateExt4Filesystem(vmem, 0, fsSize)
			if err != nil {
				return nil, nil, 0, fmt.Errorf("failed to create ext4 filesystem: %w", err)
			}
			ext4Fs = fs

			tr.log.Debug("created ext4 filesystem", "took", time.Since(start))

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

			if err := fs.AddDirectory(tr, root, regionWrapper, skipDirectories); err != nil {
				return nil, nil, 0, fmt.Errorf("failed to add directory to filesystem: %w", err)
			}

			tr.log.Debug("built filesystem", "took", time.Since(start))
		case config.FilesystemKindRaw:
			// noop
		default:
			return nil, nil, 0, fmt.Errorf("unknown filesystem kind: %s", kind)
		}

		// If we have a final disk (a persistent disk), we need to copy the memory to it.
		if final != nil {
			if feature.HasFeature(feature.FeatureFastWritePersist) {
				if _, err := vmem.WriteSparseTo(final); err != nil {
					return nil, nil, 0, fmt.Errorf("failed to copy memory to persistent filesystem: %w", err)
				}
			} else {
				if _, err := io.Copy(io.NewOffsetWriter(final, 0), io.NewSectionReader(vmem, 0, fsSize)); err != nil {
					return nil, nil, 0, fmt.Errorf("failed to copy memory to persistent filesystem: %w", err)
				}
			}
		} else {
			final = bd
		}

		return final, ext4Fs, fsSize, nil
	} else {
		tr.log.Info("opened persistent disk", "size", fsSize, "path", persistPath)

		return final, nil, fsSize, nil
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
			tr.log.Error("nbd server failed to accept", "error", err)
			return
		}

		go func(conn net.Conn) {
			minBlockSize := uint32(4096)
			preferredBlockSize := uint32(4096)
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

			// log.Debug("got nbd connection", "remote", conn.RemoteAddr().String())
			err = gonbd.Handle(conn, exportList, &gonbd.Options{
				ReadOnly:           false,
				MinimumBlockSize:   minBlockSize, // Fix for VZ on Darwin, it errors if the minimum is too large.
				PreferredBlockSize: preferredBlockSize,
				MaximumBlockSize:   maximumBlockSize,
				Log:                tr.log,
			})
			if err != nil {
				tr.log.Warn("nbd server failed to handle", "error", err)
			}
		}(conn)
	}
}

func (d *driver) startDNSServer() error {
	dnsServer := &dnsServer{
		log: d.log,
		dnsLookup: func(name string) (string, error) {
			switch name {
			case "tinyrange.":
				return d.networkGuestIP(), nil
			case "host.internal.":
				return d.networkHostGateway(), nil
			}

			d.log.Debug("doing DNS lookup", "name", name)

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

	packetConn, err := d.ns.ListenPacketInternal("udp", ":53")
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
			d.log.Error("dns: failed to start server", "error", err.Error())
		}
	}()

	return nil
}

func (d *driver) exportPort(port portInformation) error {
	if port.listenAddress == "" {
		port.listenAddress = "localhost"
	}

	targetPort := port.port
	if port.guestPort != 0 {
		targetPort = port.guestPort
	}

	if targetPort != port.port {
		d.log.Info("exporting port", "address", fmt.Sprintf("%s:%d -> %d", port.listenAddress, port.port, targetPort))
	} else {
		d.log.Info("exporting port", "address", fmt.Sprintf("%s:%d", port.listenAddress, port.port))
	}

	portListen, err := net.Listen("tcp", fmt.Sprintf("%s:%d", port.listenAddress, port.port))
	if err != nil {
		return err
	}

	go func() {
		for {
			conn, err := portListen.Accept()
			if err != nil {
				d.log.Error("failed to accept", "err", err)
				return
			}

			go func() {
				defer conn.Close()

				clientConn, err := d.ns.DialInternalContext(context.Background(), "tcp", fmt.Sprintf("%s:%d", d.networkGuestIP(), targetPort))
				if err != nil {
					// silence these errors since they are exposed to the client anyway.
					d.log.Debug("failed to dial vm port", "err", err)
					return
				}
				defer clientConn.Close()

				if err := common.Proxy(clientConn, conn, 4096); err != nil {
					d.log.Debug("failed to proxy connection", "err", err)
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

// networkGuestIP returns the guest IPv4 without CIDR suffix.
func (d *driver) networkGuestIP() string {
	cfg := d.topConfig().Network
	if cfg.GuestCIDR == "" {
		return "10.42.0.2"
	}
	// Split CIDR like "10.42.0.2/16"
	if ip, _, ok := strings.Cut(cfg.GuestCIDR, "/"); ok {
		return ip
	}
	return cfg.GuestCIDR
}

func (d *driver) networkHostGateway() string {
	if gw := d.topConfig().Network.HostGateway; gw != "" {
		return gw
	}
	return "10.42.0.1"
}

func (d *driver) networkServiceIP() string {
	if ip := d.topConfig().Network.HostService; ip != "" {
		return ip
	}
	return "10.42.0.100"
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

func (d *driver) startFileShare(mountedHostDirectories []mountInfo) error {
	if feature.HasFeature(feature.Feature9P) {
		for _, dir := range mountedHostDirectories {
			var hostDir filesystem.Directory

			if dir.Writable {
				hostDir = filesystem.Factory.NewLocalMutableDirectory(dir.HostDirectory)
			} else {
				hostDir = filesystem.Factory.NewLocalDirectory(dir.HostDirectory)
			}

			svr := p9.NewServer(hostDir, d.log)

			listen, err := d.ns.ListenInternal("tcp", fmt.Sprintf(":%d", dir.Port))
			if err != nil {
				return fmt.Errorf("failed to listen internal (9p): %w", err)
			}

			go func() {
				if err := svr.Serve(listen); err != nil {
					d.log.Error("failed to run 9p server", "err", err)
				}
			}()
		}
	} else {
		top := filesystem.Factory.NewMemoryDirectory()

		for _, dir := range mountedHostDirectories {
			name := path.Native.Base(dir.HostDirectory)

			var hostDir filesystem.Directory

			if dir.Writable {
				hostDir = filesystem.Factory.NewLocalMutableDirectory(dir.HostDirectory)
			} else {
				hostDir = filesystem.Factory.NewLocalDirectory(dir.HostDirectory)
			}

			if _, err := fsutil.CreateChild(top, name, hostDir); err != nil {
				return fmt.Errorf("failed to create child %s: %w", name, err)
			}
		}

		if len(mountedHostDirectories) > 0 {
			d.log.Info("host directories avalible via SFTP on sftp://host.internal")
		}

		svr := sftp.NewInternalServer(top, d.log, ":22")

		go func() {
			if err := svr.Run(func(network, addr string) (net.Listener, error) {
				return d.ns.ListenInternal("tcp", addr)
			}); err != nil {
				d.log.Error("failed to run sftp server", "err", err)
			}
		}()
	}

	return nil
}

func (d *driver) startSocks5Proxy(listener net.Listener) error {
	if d.ns == nil {
		return fmt.Errorf("netstack not initialized")
	}
	server := socks5.NewServer(
		socks5.WithDialAndRequest(func(ctx context.Context, network, addr string, request *socks5.Request) (net.Conn, error) {
			if network != "tcp" {
				return nil, fmt.Errorf("unsupported network: %s", network)
			}

			clientConn, err := d.ns.DialInternalContext(ctx, network, addr)
			if err != nil {
				return nil, fmt.Errorf("failed to dial vm port: %w", err)
			}

			return clientConn, nil
		}),
		socks5.WithBindHandle(func(ctx context.Context, writer io.Writer, request *socks5.Request) error {
			return fmt.Errorf("bind not supported")
		}),
		socks5.WithAssociateHandle(func(ctx context.Context, writer io.Writer, request *socks5.Request) error {
			return fmt.Errorf("associate not supported")
		}),
	)

	go func() {
		if err := server.Serve(listener); err != nil {
			d.log.Error("failed to run socks5 server", "err", err)
		}
	}()

	return nil
}

func (d *driver) exec(create func(vmm Driver) (VirtualMachineMonitor, error)) error {
	mainStart := time.Now()

	if err := d.loadBuildDatabase(); err != nil {
		return fmt.Errorf("failed to load build database: %w", err)
	}

	topConfig := d.topConfig()

	if topConfig.CPUCores == 0 || topConfig.MemoryMB == 0 {
		return fmt.Errorf("invalid config")
	}

	if topConfig.CPUCores > runtime.NumCPU() {
		return fmt.Errorf("invalid config, too many cores, got %d, max %d", topConfig.CPUCores, runtime.NumCPU())
	}

	totalMem, err := memory.TotalMemory()
	if err == nil && totalMem < uint64(topConfig.MemoryMB)*1024*1024 {
		return fmt.Errorf("not enough memory for VM, got %dmb, need %dmb", totalMem/1024/1024, topConfig.MemoryMB)
	}

	d.cpuCores = topConfig.CPUCores
	d.memoryMB = topConfig.MemoryMB

	if topConfig.AutoScale {
		autoCpus, autoMem, err := common.GetCPUAndMemoryAutoScaleConfig()
		if err != nil {
			return fmt.Errorf("failed to get auto scale config: %w", err)
		}

		if d.cpuCores < autoCpus {
			d.cpuCores = autoCpus
		}

		if d.memoryMB < autoMem {
			d.memoryMB = autoMem
		}
	}

	if topConfig.Debug {
		d.log.Warn("enabling hypervisor debug mode")
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

	d.log.Debug("built filesystem tree", "took", time.Since(start))

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

		if feature.HasFeature(feature.FeatureFastWritePersist) {
			vmem, ok := vmem.(*virtualBlockDevice)
			if !ok {
				return fmt.Errorf("failed to cast to virtual memory")
			}

			if _, err := vmem.WriteSparseTo(out); err != nil {
				return fmt.Errorf("failed to copy export filesystem: %w", err)
			}
		} else {
			pb := d.log.NewProgressBarBytes(fsSize, "exporting filesystem")
			defer pb.Close()

			if _, err := io.Copy(io.MultiWriter(pb, out), io.NewSectionReader(vmem, 0, fsSize)); err != nil {
				return fmt.Errorf("failed to copy export filesystem: %w", err)
			}
		}

		d.log.Debug("exported filesystem", "took", time.Since(start))

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
		if _, err := fsutil.Mkdir(root, volume.GuestPath); err != nil {
			return fmt.Errorf("failed to open path: %w", err)
		}
	}

	for _, volume := range append([]volumeInfo{
		{VolumeName: "root", GuestPath: "/", MinimumSizeMB: uint64(rootInfo.StorageSize)},
	}, volumes...) {
		rootDir, err := fsutil.Mkdir(root, volume.GuestPath)
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
		d.onExit = append(d.onExit, func() {
			if err := vmem.Close(); err != nil {
				d.log.Error("failed to close virtual memory", "error", err)
			}
		})

		nbdAddress := &nbdAddress{Addr: addr, Export: volume.VolumeName}

		exports = append(exports, nbdExport{
			name:    volume.VolumeName,
			backend: &vmBackend{driver: d, vm: vmem},
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
			if err := rootFilesystem.EnsurePath("/init.d"); err != nil && !errors.Is(err, filesystem.ErrExist{}) {
				return fmt.Errorf("failed to ensure path: %w", err)
			}

			mountNames := []string{"vdb", "vdc", "vdd", "vde", "vdf", "vdg"}

			mountScript := "def main():\n"
			for i, volume := range volumes {
				mountScript += fmt.Sprintf(
					"  mount(\"ext4\", \"/dev/%s\", \"%s\", ensure_path = True)\n",
					mountNames[i], volume.GuestPath,
				)
				if feature.HasFeature(feature.FeatureExt4Resize) {
					mountScript += fmt.Sprintf("  linux_ext4_try_resize(\"%s\")\n", volume.GuestPath)
				}
			}

			if err := rootFilesystem.WriteFile("/init.d/mount.star", []byte(mountScript)); err != nil {
				return fmt.Errorf("failed to write file: %w", err)
			}
		}
	}

	go d.nbdLoop(listener, exports...)

	ns := netstack.New(d.log)
	// Configure netstack addressing and internet policy from config.
	_ = ns.SetNetworkParams(d.networkHostGateway(), d.networkGuestIP(), d.networkServiceIP(), d.topConfig().Network.AllowInternet)
	if mac := d.topConfig().Network.GuestMAC; mac != "" {
		if err := ns.SetGuestMAC(mac); err != nil {
			return fmt.Errorf("invalid network.guest_mac: %w", err)
		}
	}
	d.ns = ns

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
	if err := d.startDNSServer(); err != nil {
		return fmt.Errorf("failed to start DNS server: %w", err)
	}

	// Export ports.
	for _, port := range exportedPorts {
		if err := d.exportPort(port); err != nil {
			return fmt.Errorf("failed to export port: %w", err)
		}
	}

	// Expose SSH if requested (listen <addr>:<port> -> guest:2222).
	if d.sshPort > 0 {
		addr := d.sshListenAddress
		if addr == "" {
			addr = "localhost"
		}
		if err := d.exportPort(portInformation{listenAddress: addr, port: d.sshPort, guestPort: 2222}); err != nil {
			return fmt.Errorf("failed to export ssh port: %w", err)
		}
	}

	// Set the kernel.
	if topConfig.Kernel != nil {
		kernelFile, err := d.ResolveReference(*topConfig.Kernel)
		if err != nil {
			return fmt.Errorf("failed to resolve kernel file: %w", err)
		}

		kernelFilename, err := filesystem.GetHostFilename(kernelFile)
		if err != nil {
			return fmt.Errorf("failed to get host filename: %w", err)
		}

		d.kernel = &localFile{
			driver:   d,
			filename: kernelFilename,
		}
	}

	// Set the initrd.
	if topConfig.InitFilesystem != nil {
		initFsFile, err := d.ResolveReference(*topConfig.InitFilesystem)
		if err != nil {
			return fmt.Errorf("failed to resolve init filesystem file: %w", err)
		}

		initFsFilename, err := filesystem.GetHostFilename(initFsFile)
		if err != nil {
			return fmt.Errorf("failed to get host filename: %w", err)
		}

		d.initRamFs = &localFile{
			driver:   d,
			filename: initFsFilename,
		}
	}

	// Create the virtual machine monitor.
	vmm, err := create(d)
	if err != nil {
		return fmt.Errorf("failed to create virtual machine monitor: %w", err)
	}

	// Start the file share.
	// Do this after the VM is created so the driver can add files viable to the VM.
	if err := d.startFileShare(mountedHostDirectories); err != nil {
		return fmt.Errorf("failed to start file share: %w", err)
	}

	if d.socks5Proxy != "" && d.socks5Listener == nil {
		listener, err := net.Listen("tcp", d.socks5Proxy)
		if err != nil {
			return fmt.Errorf("failed to listen on socks5 proxy: %w", err)
		}

		d.socks5Listener = listener
	}
	if d.socks5Listener != nil {
		if err := d.startSocks5Proxy(d.socks5Listener); err != nil {
			return fmt.Errorf("failed to start socks5 proxy: %w", err)
		}
	}

	d.log.Debug("starting virtual machine", "took", time.Since(start))

	d.onExit = append(d.onExit, func() {
		if err := vmm.Shutdown(); err != nil {
			d.log.Error("failed to shutdown virtual machine", "err", err)
		}
	})

	d.log.Debug("running virtual machine", "initTime", time.Since(mainStart))

	return d.runInteraction(
		vmm,
		secureSSH,
	)
}

func (d *driver) execProxy(create func(vmm ProxyDriver) (ProxyMonitor, error)) error {
	if err := d.loadBuildDatabase(); err != nil {
		return fmt.Errorf("failed to load build database: %w", err)
	}

	secureSSH := SecureSSHConfig{
		Password: config.INSECURE_SSH_PASSWORD,
	}

	inst, err := create(d)
	if err != nil {
		return fmt.Errorf("failed to create proxy instance: %w", err)
	}

	d.onExit = append(d.onExit, func() {
		if err := inst.Shutdown(); err != nil {
			d.log.Error("failed to shutdown virtual machine", "err", err)
		}
	})

	d.ns = inst.NetStack()

	return d.runInteraction(inst, secureSSH)
}

func (d *driver) runInteraction(
	vmm VirtualMachineMonitor,
	secureSSH SecureSSHConfig,
) error {
	var err error

	defer func() {
		for _, fn := range d.onExit {
			fn()
		}
	}()

	switch d.Interaction() {
	case config.InteractionSSH, config.InteractionVNC:
		exited := &exitNotify{}

		go func() {
			if err := vmm.Run(d.log, d.debug); err != nil {
				d.log.Error("failed to run virtual machine", "err", err)
				os.Exit(1)
			}

			// VM has exited. Send an event to notify.
			exited.Set()
		}()

		if d.Interaction() == config.InteractionVNC {
			go runVncClient(d.ns, d.log, fmt.Sprintf("%s:5901", d.networkGuestIP()))
		}

		err = connectOverSsh(d.ns, d.log, fmt.Sprintf("%s:2222", d.networkGuestIP()), "root", secureSSH, exited)
		if err != nil {
			return fmt.Errorf("failed to connect over ssh: %w", err)
		}

		return nil
	case config.InteractionRemote:
		if err := vmm.Run(d.log, d.debug); err != nil {
			return fmt.Errorf("failed to run virtual machine: %w", err)
		}

		return nil
	case config.InteractionSerial:
		if err := vmm.Run(d.log, true); err != nil {
			return fmt.Errorf("failed to run virtual machine: %w", err)
		}

		return nil
	case config.InteractionWebSSH, config.InteractionWebSSHMinimal, config.InteractionWebSSHNoBrower:
		go func() {
			if err := vmm.Run(d.log, d.debug); err != nil {
				d.log.Error("failed to run virtual machine", "err", err)
				os.Exit(1)
			}
		}()

		return runWebSsh(d.ns, d.log, fmt.Sprintf("%s:2222", d.networkGuestIP()), "root", secureSSH, strings.TrimPrefix(string(d.Interaction()), "webssh,"))
	default:
		return fmt.Errorf("unsupported interaction mode: %s", d.Interaction())
	}
}

func (d *driver) addConfig(p string) error {
	var (
		cfg     config.TinyRangeConfig
		absPath string
	)

	if p == "-" {
		if err := json.NewDecoder(os.Stdin).Decode(&cfg); err != nil {
			return fmt.Errorf("failed to decode config from stdin: %w", err)
		}

		absPath = "<stdin>"
	} else {
		f, err := os.Open(p)
		if err != nil {
			return fmt.Errorf("failed to open config file: %w", err)
		}
		defer f.Close()

		absPath, err = path.Native.Abs(p)
		if err != nil {
			return fmt.Errorf("failed to get absolute path: %w", err)
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
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("failed to validate config: %w", err)
	}

	if cfg.TinyRangeVersion != "" && !d.noValidateTinyRangeVersion {
		nfo, ok := goDebug.ReadBuildInfo()
		if ok {
			if cfg.TinyRangeVersion != nfo.Main.Version {
				return fmt.Errorf(
					"config TinyRangeVersion %s does not match current version %s, please regenerate your config",
					cfg.TinyRangeVersion, nfo.Main.Version)
			}
		}
	}

	d.configs = append(d.configs, cfg)
	d.configFilenames = append(d.configFilenames, absPath)

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

	if feature.HasFeature(feature.FeatureNoAccelerate) {
		return false
	}

	return accelerate.SupportsAcceleration(d.log)
}

func (d *driver) HostOperatingSystem() string                { return runtime.GOOS }
func (d *driver) GuestArchitecture() config.CPUArchitecture  { return d.topConfig().Architecture }
func (d *driver) DiskImages() []File                         { return d.diskImages }
func (d *driver) InitRamFs() File                            { return d.initRamFs }
func (d *driver) Verbose() bool                              { return common.IsVerbose() }
func (d *driver) Experimental() []string                     { return feature.GetFeatureFlags() }
func (d *driver) Interaction() config.InteractionKind        { return d.topConfig().Interaction }
func (d *driver) NetworkInterface() NetworkInterface         { return d.networkInterface }
func (d *driver) Kernel() File                               { return d.kernel }
func (d *driver) CPUCores() int                              { return d.cpuCores }
func (d *driver) MemoryMB() int                              { return d.memoryMB }
func (d *driver) URL() *url.URL                              { return d.driverUrl }
func (d *driver) Configs() []config.TinyRangeConfig          { return d.configs }
func (d *driver) BuildDatabase() build2.BuildCacheFilesystem { return d.dbBuildDir }

func (d *driver) RootArchitecture() config.CPUArchitecture {
	return d.topConfig().RootArchitecture
}

func (d *driver) EnsureFile(contents []byte) (File, error) {
	if len(contents) == 0 {
		return nil, fmt.Errorf("empty contents")
	}

	hash := hash.GetSha256Hash(contents)

	path := path.Native.Join(d.buildDir, string(hash)+".bin")

	d.log.Debug("ensure file", "path", path, "length", len(contents))

	if ok, _ := common.Exists(path); !ok {
		if err := os.WriteFile(path, contents, os.ModePerm); err != nil {
			return &localFile{driver: d, filename: path}, err
		}
	}

	return &localFile{driver: d, filename: path}, nil
}

var (
	_ Driver                         = &driver{}
	_ filesystem.ExtendedFileMethods = &driver{}
)

var DriverFlags = flag.NewFlagSet(os.Args[0], flag.ExitOnError)

var (
	doPrepare                  = DriverFlags.Bool("prepare", false, "prepare the driver and check if it is runnable")
	buildDir                   = DriverFlags.String("build-dir", common.GetDefaultBuildDir(), "the build directory")
	debug                      = DriverFlags.Bool("debug", false, "enable debug mode")
	verbose                    = DriverFlags.Bool("verbose", false, "enable verbose mode")
	experimental               = DriverFlags.String("experimental", "", "enable comma separated experimental features")
	secureSSH                  = DriverFlags.String("secure-ssh", "", "Specify a local file to save a secure SSH config to. This will set a random persistent host key and root password.")
	instanceName               = DriverFlags.String("name", "", "Instance name used to derive persistent SSH keys and state.")
	exposeSSHFlag              = DriverFlags.String("expose-ssh", "", "Expose guest SSH. Accepts '<host>:<port>' or '<port>' (defaults to localhost). Forwards to guest 2222.")
	sshKeyPath                 = DriverFlags.String("ssh-key", "", "Path to SSH identity (private key or .pub) to authorize and use for internal SSH.")
	persistPath                = DriverFlags.String("persist-path", "", "Specify a path to save VM files to.")
	exportFsPath               = DriverFlags.String("exportfs", "", "Export the filesystem to a file.")
	dumpFsPath                 = DriverFlags.String("dumpfs", "", "Dump the filename and offset of any reads from the filesystem to a CSV file.")
	wireguardUrl               = DriverFlags.String("wireguard-url", "", "URL to fetch wireguard config from.")
	nbdBlockSize               = DriverFlags.Int("nbd-block-size", 0, "Override the preferred and maximum block size for the NBD server. This can have major performance implications.")
	packetCapturePath          = DriverFlags.String("packet-capture", "", "Path to write packet capture in pcap format to.")
	driverUrl                  = DriverFlags.String("url", "", "The URL of the driver to create.")
	proxy                      = DriverFlags.String("proxy", "", "The URL of a proxy host to fetch the configuration from.")
	socks5Proxy                = DriverFlags.String("socks5-proxy", "", "The URL of a socks5 proxy to listen.")
	noValidateTinyRangeVersion = DriverFlags.Bool("no-validate-tinyrange-version", false, "Do not validate the TinyRange version. This is useful for development, but should not be used in production.")
)

func initCommon(
	prepare func(vmm ProxyDriver) (PrepareResult, error),
) (*driver, error) {
	if err := DriverFlags.Parse(os.Args[1:]); err != nil {
		return nil, err
	}

	if *verbose {
		common.EnableVerbose()
	}

	if err := common.SetExperimental(strings.Split(*experimental, ",")); err != nil {
		return nil, err
	}

	driverUrl, err := url.Parse(*driverUrl)
	if err != nil {
		return nil, fmt.Errorf("failed to parse driver URL: %w", err)
	}

	driver := &driver{
		log:                        log.Default(),
		buildDir:                   *buildDir,
		debug:                      *debug,
		secureSSH:                  *secureSSH,
		name:                       *instanceName,
		sshKey:                     *sshKeyPath,
		persistPath:                *persistPath,
		exportFsPath:               *exportFsPath,
		dumpFsPath:                 *dumpFsPath,
		wireguardUrl:               *wireguardUrl,
		nbdBlockSize:               *nbdBlockSize,
		packetCapturePath:          *packetCapturePath,
		socks5Proxy:                *socks5Proxy,
		noValidateTinyRangeVersion: *noValidateTinyRangeVersion,
		driverUrl:                  driverUrl,
	}

	// If a name is provided and no secure-ssh path, derive a persistent path from the build directory.
	if driver.secureSSH == "" && driver.name != "" {
		persistDir := path.Native.Join(driver.buildDir, "secure-ssh")
		if err := os.MkdirAll(persistDir, 0o700); err != nil {
			return nil, fmt.Errorf("failed to create secure-ssh dir: %w", err)
		}

		driver.secureSSH = path.Native.Join(persistDir, driver.name+".json")
	}

	// Environment overrides for automation and login passthrough
	if driver.name == "" {
		if v := os.Getenv("TINYRANGE_NAME"); v != "" {
			driver.name = v
			if driver.secureSSH == "" {
				persistDir := path.Native.Join(driver.buildDir, "secure-ssh")
				if err := os.MkdirAll(persistDir, 0o700); err != nil {
					return nil, fmt.Errorf("failed to create secure-ssh dir: %w", err)
				}

				driver.secureSSH = path.Native.Join(persistDir, driver.name+".json")
			}
		}
	}
	if driver.sshKey == "" {
		if v := os.Getenv("TINYRANGE_SSH_KEY"); v != "" {
			driver.sshKey = v
		}
	}

	// Parse expose-ssh from flag or env
	expose := *exposeSSHFlag
	if expose == "" {
		if v := os.Getenv("TINYRANGE_EXPOSE_SSH"); v != "" {
			expose = v
		}
	}
	if expose != "" {
		host := "localhost"
		portStr := expose
		if strings.Contains(portStr, ":") {
			parts := strings.SplitN(portStr, ":", 2)
			if len(parts) != 2 || parts[1] == "" {
				return nil, fmt.Errorf("invalid --expose-ssh value: %s", expose)
			}
			host, portStr = parts[0], parts[1]
			if host == "" {
				host = "localhost"
			}
		}
		p, err := strconv.Atoi(portStr)
		if err != nil || p <= 0 || p > 65535 {
			return nil, fmt.Errorf("invalid --expose-ssh port: %s", portStr)
		}
		driver.sshListenAddress = host
		driver.sshPort = p
	}

	if *doPrepare {
		out, err := prepare(driver)
		if err != nil {
			driver.log.Error("fatal", "err", err)
			os.Exit(1)
		}

		enc, err := json.Marshal(out)
		if err != nil {
			return nil, err
		}

		if _, err := os.Stdout.Write(enc); err != nil {
			return nil, err
		}
	}

	if *proxy != "" {
		// do a request to the proxy server to get the config.
		var req config.ProxyLoadRequest

		listen, err := net.Listen("tcp", "localhost:0")
		if err != nil {
			return nil, fmt.Errorf("failed to listen: %w", err)
		}
		driver.socks5Listener = listen

		req.Socks5Address = listen.Addr().String()

		buf := bytes.NewBuffer(nil)
		if err := json.NewEncoder(buf).Encode(&req); err != nil {
			return nil, fmt.Errorf("failed to encode request: %w", err)
		}

		client := http.DefaultClient

		request, err := http.NewRequest("POST", *proxy, buf)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "application/json")

		resp, err := client.Do(request)
		if err != nil {
			return nil, fmt.Errorf("failed to do request: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("failed to get config from proxy: %s", resp.Status)
		}

		defer resp.Body.Close()

		var configs []config.TinyRangeConfig

		if err := json.NewDecoder(resp.Body).Decode(&configs); err != nil {
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}

		for _, cfg := range configs {
			if err := cfg.Validate(); err != nil {
				return nil, fmt.Errorf("failed to validate config: %w", err)
			}

			driver.configs = append(driver.configs, cfg)
			driver.configFilenames = append(driver.configFilenames, *proxy)
		}

		if len(driver.configs) == 0 {
			return nil, fmt.Errorf("no configs provided")
		}
	} else {
		for _, arg := range DriverFlags.Args() {
			if err := driver.addConfig(arg); err != nil {
				return nil, err
			}
		}

		if len(driver.configs) == 0 {
			return nil, fmt.Errorf("no configs provided")
		}
	}

	return driver, nil
}

func ProxyEntry(
	prepare func(vmm ProxyDriver) (PrepareResult, error),
	create func(vmm ProxyDriver) (ProxyMonitor, error),
) {
	if goboot.MaybeExecInit() {
		return
	}

	log := log.Default()

	if os.Getenv("TINYRANGE_VERBOSE") == "on" {
		if err := common.EnableVerbose(); err != nil {
			log.Error("failed to enable verbose logging", "err", err)
			os.Exit(1)
		}
	}

	driver, err := initCommon(prepare)
	if err != nil {
		log.Error("driver fatal", "err", err)
		os.Exit(1)
	}

	if err := driver.execProxy(create); err != nil {
		log.Error("driver fatal", "err", err)
		os.Exit(1)
	}
}

func Entry(
	prepare func(vmm ProxyDriver) (PrepareResult, error),
	create func(vmm Driver) (VirtualMachineMonitor, error),
) {
	if goboot.MaybeExecInit() {
		return
	}

	log := log.Default()

	if os.Getenv("TINYRANGE_VERBOSE") == "on" {
		if err := common.EnableVerbose(); err != nil {
			log.Error("failed to enable verbose logging", "err", err)
			os.Exit(1)
		}
	}

	driver, err := initCommon(prepare)
	if err != nil {
		log.Error("driver fatal", "err", err)
		os.Exit(1)
	}

	if err := driver.exec(create); err != nil {
		log.Error("driver fatal", "err", err)
		os.Exit(1)
	}
}

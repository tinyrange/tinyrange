package builder

import (
	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/hash"
)

// Build Filesystem exports a series of directives into a filesystem format.
// The build result is the built filesystem.
type BuildFsParameters struct {
	Directives []common.Directive // A list of directives to build the filesystem from.
	Kind       string             // The kind of filesystem to create (initramfs,tar,fragments)
}

// Build Virtual Machine uses TinyRange to run a virtual machine with a root
// filesystem provided by a list of directives.
// The output is either nothing or a file from the virtual machine.
type BuildVmParameters struct {
	Directives   []common.Directive // A list of directives to build the root filesystem from.
	OutputFile   string             // The name inside of the guest of the file to copy as the build result.
	Architecture string             // The CPU Architecture of the guest. If null defaults to the host architecture.

	// TODO(joshua): Allow customizing the hypervisor, and startup script.
	Kernel      common.BuildDefinition // A build definition that creates the kernel.
	InitRamFs   common.BuildDefinition // A build definition that creates the initial ram filesystem.
	CpuCores    int                    // The number of CPU cores to allocate to the virtual machine.
	MemoryMB    int                    // The amount of RAM in the virtual machine in megabytes.
	StorageSize int                    // The amount of storage the root device will have in megabytes.
	Interaction string                 // How will the virtual machine be interacted with (ssh, serial)
	Debug       bool                   // Redirect hypervisor input to the host. The VM will exit after it completes initialization.
}

// Build Emulator uses a internal shell emulator to run simple shell scripts with support from
// Starlark to stub applications. The Directives and output file work the name way as
// BuildVmParameters.
type BuildEmulatorParameters struct {
	Directives []common.Directive // A list of directives to build the root filesystem from.
	OutputFile string             // The name inside of the guest of the file to copy as the build result.

	ScriptFilename string // The filename of the script used to run this builder.
	CreateName     string // The name of the create function.
}

// Decompress a build result.
type DecompressFileParameters struct {
	Base common.BuildDefinition // The build result to decompress.
	Kind string                 // The compression format to use (.xz)
}

// Download a file from the internet.
type FetchHttpParameters struct {
	Url        string // The URL to download (can start with mirror:// if a mirror is registered)
	ExpireTime int64  // How long before the file is considered expired and will be redownloaded.
}

// Make a request to a OCI registry.
// This is a internal type that is attached to a context to persist the authentication token between requests.
type RegistryRequestParameters struct {
	Url        string
	ExpireTime int64
	Accept     []string
}

// Download a image from a OCI registry.
// The output is a serialized copy of FetchOciImageDefinition.
type FetchOciImageParameters struct {
	Registry     string
	Image        string
	Tag          string
	Architecture string
}

// Copy a file to the build output directory.
type FileParameters struct {
	File filesystem.File
}

// Constant hash needs to be manually replicated by constructing a object.
type ConstantHashParameters struct {
	Hash string
}

// Extract a single file from a archive.
type ExtractFileParameters struct {
	Base common.BuildDefinition
	Name string
}

// Create a installation plan using a given builder.
// The result is a serialized version of PlanDefinition which contains a list of fragments.
type PlanParameters struct {
	Builder      string                // A registered builder to use to create the installation plan.
	Architecture string                // A CPUArchitecture. If not specified then use the host architecture.
	Search       []common.PackageQuery // A list of packages to query to make the installation plan.
	TagList      common.TagList        // A list of tags used to modify and configure the plan.
}

// Read a archive in a compressed format.
// The output is a file in the native archive format (it still has to be read with ReadArchiveFromFile).
type ReadArchiveParameters struct {
	Base common.BuildDefinition // The definition used as a base.
	// The compression kind of the input file (supports .gz, .zst, and .xz compression
	// and .tar, .cpio, and .ar archive formats)
	Kind string
}

// Execute a builder defined in Starlark.
type StarParameters struct {
	ScriptFilename string                   // The filename of the script used to run this builder.
	BuilderName    string                   // The name of the builder function.
	Arguments      []hash.SerializableValue // The arguments passed to the function. These must be convertible to starlark.Value.
}

func (b BuildFsParameters) SerializableType() string         { return "BuildFsParameters" }
func (b BuildVmParameters) SerializableType() string         { return "BuildVmParameters" }
func (b BuildEmulatorParameters) SerializableType() string   { return "BuildEmulatorParameters" }
func (d DecompressFileParameters) SerializableType() string  { return "DecompressFileParameters" }
func (f FetchHttpParameters) SerializableType() string       { return "FetchHttpParameters" }
func (r RegistryRequestParameters) SerializableType() string { return "RegistryRequestParameters" }
func (f FetchOciImageParameters) SerializableType() string   { return "FetchOciImageParameters" }
func (f FileParameters) SerializableType() string            { return "FileParameters" }
func (f ConstantHashParameters) SerializableType() string    { return "ConstantHashParameters" }
func (f ExtractFileParameters) SerializableType() string     { return "ExtractFileParameters" }
func (p PlanParameters) SerializableType() string            { return "PlanParameters" }
func (r ReadArchiveParameters) SerializableType() string     { return "ReadArchiveParameters" }
func (s StarParameters) SerializableType() string            { return "StarParameters" }

var (
	_ hash.SerializableValue = BuildVmParameters{}
	_ hash.SerializableValue = BuildFsParameters{}
	_ hash.SerializableValue = BuildEmulatorParameters{}
	_ hash.SerializableValue = DecompressFileParameters{}
	_ hash.SerializableValue = FetchHttpParameters{}
	_ hash.SerializableValue = RegistryRequestParameters{}
	_ hash.SerializableValue = FetchOciImageParameters{}
	_ hash.SerializableValue = FileParameters{}
	_ hash.SerializableValue = ConstantHashParameters{}
	_ hash.SerializableValue = ExtractFileParameters{}
	_ hash.SerializableValue = PlanParameters{}
	_ hash.SerializableValue = ReadArchiveParameters{}
	_ hash.SerializableValue = StarParameters{}
)

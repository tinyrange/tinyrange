package builder

import (
	"time"

	"github.com/tinyrange/tinyrange/pkg/common"
)

// Build Filesystem exports a series of directives into a filesystem format.
// The build result is the built filesystem.
type BuildFsParameters struct {
	Directives []common.Directive // A list of directives to build the filesystem from.
	Kind       string             // The kind of filesystem to create (initramfs)
}

// Build Virtual Machine uses TinyRange to run a virtual machine with a root
// filesystem provided by a list of directives.
// The output is either nothing or a file from the virtual machine.
type BuildVmParameters struct {
	Directives []common.Directive // A list of directives to build the root filesystem from.
	OutputFile string             // The name inside of the guest of the file to copy as the build result.

	// TODO(joshua): Allow customizing the hypervisor, and startup script.
	Kernel      common.BuildDefinition // A build definition that creates the kernel.
	InitRamFs   common.BuildDefinition // A build definition that creates the initial ram filesystem.
	CpuCores    int                    // The number of CPU cores to allocate to the virtual machine.
	MemoryMB    int                    // The amount of RAM in the virtual machine in megabytes.
	StorageSize int                    // The amount of storage the root device will have in megabytes.
	Interaction string                 // How will the virtual machine be interacted with (ssh, serial)
}

// Create a archive from a filesystem directory.
type CreateArchiveParameters struct {
	Dir common.Directory
}

// Decompress a build result.
type DecompressFileParameters struct {
	Base common.BuildDefinition // The build result to decompress.
	Kind string                 // The compression format to use (.xz)
}

// Download a file from the internet.
type FetchHttpParameters struct {
	Url        string        // The URL to download (can start with mirror:// if a mirror is registered)
	ExpireTime time.Duration // How long before the file is considered expired and will be redownloaded.
}

// Make a request to a OCI registry.
// This is a internal type that is attached to a context to persist the authentication token between requests.
type RegistryRequestParameters struct {
	Url        string
	ExpireTime time.Duration
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
	File common.File
}

// Create a installation plan using a given builder.
// The result is a serialized version of PlanDefinition which contains a list of fragments.
type PlanParameters struct {
	Builder string                // A registered builder to use to create the installation plan.
	Search  []common.PackageQuery // A list of packages to query to make the installation plan.
	TagList common.TagList        // A list of tags used to modify and configure the plan.
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
	ScriptFile  common.BuildDefinition // A build definition for the script file to load and execute.
	BuilderName string                 // The name of the builder function.
	Arguments   []any                  // The arguments passed to the function. These must be convertible to starlark.Value.
}

func (b BuildFsParameters) TagParameters()         { panic("unimplemented") }
func (b BuildVmParameters) TagParameters()         { panic("unimplemented") }
func (c CreateArchiveParameters) TagParameters()   { panic("unimplemented") }
func (d DecompressFileParameters) TagParameters()  { panic("unimplemented") }
func (f FetchHttpParameters) TagParameters()       { panic("unimplemented") }
func (r RegistryRequestParameters) TagParameters() { panic("unimplemented") }
func (f FetchOciImageParameters) TagParameters()   { panic("unimplemented") }
func (f FileParameters) TagParameters()            { panic("unimplemented") }
func (p PlanParameters) TagParameters()            { panic("unimplemented") }
func (r ReadArchiveParameters) TagParameters()     { panic("unimplemented") }
func (s StarParameters) TagParameters()            { panic("unimplemented") }

var (
	_ common.BuildDefinitionParameters = BuildVmParameters{}
	_ common.BuildDefinitionParameters = BuildFsParameters{}
	_ common.BuildDefinitionParameters = CreateArchiveParameters{}
	_ common.BuildDefinitionParameters = DecompressFileParameters{}
	_ common.BuildDefinitionParameters = FetchHttpParameters{}
	_ common.BuildDefinitionParameters = RegistryRequestParameters{}
	_ common.BuildDefinitionParameters = FetchOciImageParameters{}
	_ common.BuildDefinitionParameters = FileParameters{}
	_ common.BuildDefinitionParameters = PlanParameters{}
	_ common.BuildDefinitionParameters = ReadArchiveParameters{}
	_ common.BuildDefinitionParameters = StarParameters{}
)

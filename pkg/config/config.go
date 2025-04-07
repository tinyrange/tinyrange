package config

import (
	"fmt"
	"runtime"

	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/path"
)

// Used as the default password when one is not provided.
const INSECURE_SSH_PASSWORD = "insecurepassword"

type CPUArchitecture string

const (
	ArchInvalid CPUArchitecture = ""
	ArchX8664   CPUArchitecture = "x86_64"
	ArchARM64   CPUArchitecture = "aarch64"
)

func (arch CPUArchitecture) IsNative() bool {
	return arch == HostArchitecture
}

func ArchitectureFromString(s string) (CPUArchitecture, error) {
	switch s {
	case "x86_64":
		return ArchX8664, nil
	case "aarch64":
		return ArchARM64, nil
	case "":
		return ArchInvalid, nil
	default:
		return ArchInvalid, fmt.Errorf("could not parse architecture: %s", s)
	}
}

var HostArchitecture = getHostArchitecture()

func getHostArchitecture() CPUArchitecture {
	switch runtime.GOARCH {
	case "amd64":
		return ArchX8664
	case "arm64":
		return ArchARM64
	default:
		panic("unknown architecture: " + runtime.GOARCH)
	}
}

type DatabaseReference struct {
	Hash     string `json:"hash" yaml:"hash"`
	Filename string `json:"filename" yaml:"filename"`
}

func (db DatabaseReference) Validate() error {
	if db.Hash == "" {
		return fmt.Errorf("hash is required")
	}

	if db.Filename == "" {
		return fmt.Errorf("filename is required")
	}

	return nil
}

type LocalFileFragment struct {
	HostFilename  string `json:"host_filename" yaml:"host_filename"`
	GuestFilename string `json:"guest_filename" yaml:"guest_filename"`
	Executable    bool   `json:"executable" yaml:"executable"`
}

func (f LocalFileFragment) Validate() error {
	if f.HostFilename == "" {
		return fmt.Errorf("host_filename is required")
	}

	// the host filename has to be absolute
	if !path.Native.IsAbs(f.HostFilename) {
		return fmt.Errorf("host_filename must be absolute: %s", f.HostFilename)
	}

	if f.GuestFilename == "" {
		return fmt.Errorf("guest_filename is required")
	}

	return nil
}

type DatabaseFileFragment struct {
	DatabaseReference DatabaseReference `json:"host_filename" yaml:"host_filename"`
	GuestFilename     string            `json:"guest_filename" yaml:"guest_filename"`
	Executable        bool              `json:"executable" yaml:"executable"`
}

func (f DatabaseFileFragment) Validate() error {
	if err := f.DatabaseReference.Validate(); err != nil {
		return fmt.Errorf("invalid host_filename: %w", err)
	}

	if f.GuestFilename == "" {
		return fmt.Errorf("guest_filename is required")
	}

	return nil
}

type FileContentsFragment struct {
	Contents       []byte `json:"contents" yaml:"contents"`
	StringContents string `json:"string_contents" yaml:"string_contents"`
	GuestFilename  string `json:"guest_filename" yaml:"guest_filename"`
	Executable     bool   `json:"executable" yaml:"executable"`
}

func (f FileContentsFragment) Validate() error {
	if f.GuestFilename == "" {
		return fmt.Errorf("guest_filename is required")
	}

	if len(f.Contents) == 0 && f.StringContents == "" {
		return fmt.Errorf("contents or string_contents is required")
	}

	return nil
}

type ArchiveFragment struct {
	DatabaseReference DatabaseReference `json:"host_filename" yaml:"host_filename"`
	Target            string            `json:"target" yaml:"target"`
}

func (f ArchiveFragment) Validate() error {
	if err := f.DatabaseReference.Validate(); err != nil {
		return fmt.Errorf("invalid host_filename: %w", err)
	}

	return nil
}

type Archive2Fragment struct {
	IndexReference    DatabaseReference `json:"index_host_filename" yaml:"index_host_filename"`
	ContentsReference DatabaseReference `json:"contents_host_filename" yaml:"contents_host_filename"`
	Target            string            `json:"target" yaml:"target"`
}

func (f Archive2Fragment) Validate() error {
	if err := f.IndexReference.Validate(); err != nil {
		return fmt.Errorf("invalid index host_filename: %w", err)
	}

	if err := f.ContentsReference.Validate(); err != nil {
		return fmt.Errorf("invalid contents host_filename: %w", err)
	}

	return nil
}

type RunCommandFragment struct {
	Command string `json:"command" yaml:"command"`
	Raw     bool   `json:"raw" yaml:"raw"`
}

func (f RunCommandFragment) Validate() error {
	if f.Command == "" {
		return fmt.Errorf("command is required")
	}

	return nil
}

type StartServiceCommandFragment struct {
	Command string `json:"command" yaml:"command"`
}

func (f StartServiceCommandFragment) Validate() error {
	if f.Command == "" {
		return fmt.Errorf("command is required")
	}

	return nil
}

type AddInitScriptFragment struct {
	GuestFilename string `json:"guest_filename" yaml:"guest_filename"`
}

func (f AddInitScriptFragment) Validate() error {
	if f.GuestFilename == "" {
		return fmt.Errorf("guest_filename is required")
	}

	return nil
}

type EnvironmentFragment struct {
	Variables []string `json:"variables" yaml:"variables"`
}

func (f EnvironmentFragment) Validate() error {
	return nil
}

type RunStarlarkScriptFragment struct {
	Script string `json:"script" yaml:"script"`
}

func (f RunStarlarkScriptFragment) Validate() error {
	if f.Script == "" {
		return fmt.Errorf("script is required")
	}

	return nil
}

type BuiltinFragment struct {
	Name          string          `json:"builtin" yaml:"builtin"`
	Architecture  CPUArchitecture `json:"architecture" yaml:"architecture"`
	GuestFilename string          `json:"guest_filename" yaml:"guest_filename"`
}

func (f BuiltinFragment) Validate() error {
	if f.Name == "" {
		return fmt.Errorf("builtin is required")
	}

	if f.GuestFilename == "" {
		return fmt.Errorf("guest_filename is required")
	}

	if f.Architecture == ArchInvalid {
		return fmt.Errorf("invalid architecture: %s", f.Architecture)
	}

	return nil
}

type ExportPortFragment struct {
	Name string `json:"name" yaml:"name"`
	Port int    `json:"port" yaml:"port"`
}

func (f ExportPortFragment) Validate() error {
	if f.Name == "" {
		return fmt.Errorf("name is required")
	}

	if f.Port <= 0 {
		return fmt.Errorf("port must be greater than 0")
	}

	return nil
}

type DefaultInteractiveFragment struct {
	Args []string `json:"args"`
}

func (f DefaultInteractiveFragment) Validate() error {
	if len(f.Args) == 0 {
		return fmt.Errorf("args is required")
	}

	return nil
}

type MountHostDirectoryFragment struct {
	HostDirectory string `json:"host_directory" yaml:"host_directory"`
	Port          int    `json:"name" yaml:"name"`
	Writable      bool   `json:"writable" yaml:"writable"`
}

func (f MountHostDirectoryFragment) Validate() error {
	if f.HostDirectory == "" {
		return fmt.Errorf("host_directory is required")
	}

	// the host directory has to be absolute
	if !path.Native.IsAbs(f.HostDirectory) {
		return fmt.Errorf("host_directory must be absolute: %s", f.HostDirectory)
	}

	return nil
}

type KernelFragment struct {
	KernelReference    *DatabaseReference `json:"kernel_filename" yaml:"kernel_filename"`
	InitramfsReference *DatabaseReference `json:"initramfs_filename" yaml:"initramfs_filename"`
}

func (f KernelFragment) Validate() error {
	if f.KernelReference != nil {
		if err := f.KernelReference.Validate(); err != nil {
			return fmt.Errorf("invalid kernel_filename: %w", err)
		}
	}

	if f.InitramfsReference != nil {
		if err := f.InitramfsReference.Validate(); err != nil {
			return fmt.Errorf("invalid initramfs_filename: %w", err)
		}
	}

	return nil
}

type AddVolumeFragment struct {
	VolumeName    string `json:"volume_name" yaml:"volume_name"`
	GuestPath     string `json:"guest_path" yaml:"guest_path"`
	MinimumSizeMB uint64 `json:"minimum_size_mb" yaml:"minimum_size_mb"`
	Persist       bool   `json:"persist" yaml:"persist"`
}

func (f AddVolumeFragment) Validate() error {
	if f.VolumeName == "" {
		return fmt.Errorf("volume_name is required")
	}

	if f.GuestPath == "" {
		return fmt.Errorf("guest_path is required")
	}

	if f.MinimumSizeMB <= 0 {
		return fmt.Errorf("minimum_size_mb must be greater than 0")
	}

	return nil
}

type Fragment struct {
	// Not supported by TinyRange directly.
	RunCommand          *RunCommandFragment          `json:"run_command,omitempty" yaml:"run_command"`
	StartServiceCommand *StartServiceCommandFragment `json:"start_service,omitempty" yaml:"start_service"`
	AddInitScript       *AddInitScriptFragment       `json:"add_init_script,omitempty" yaml:"add_init_script"`
	Environment         *EnvironmentFragment         `json:"environment,omitempty" yaml:"environment"`
	RunStarlarkScript   *RunStarlarkScriptFragment   `json:"run_starlark_script,omitempty" yaml:"run_starlark_script"`

	// Supported Directly
	DefaultInteractive *DefaultInteractiveFragment `json:"interactive,omitempty" yaml:"interactive"`
	LocalFile          *LocalFileFragment          `json:"local_file,omitempty" yaml:"local_file"`
	DatabaseFile       *DatabaseFileFragment       `json:"database_file,omitempty" yaml:"database_file"`
	FileContents       *FileContentsFragment       `json:"file_contents,omitempty" yaml:"file_contents"`
	Archive            *ArchiveFragment            `json:"archive,omitempty" yaml:"archive"`
	Archive2           *Archive2Fragment           `json:"archive2,omitempty" yaml:"archive2"`
	Builtin            *BuiltinFragment            `json:"builtin,omitempty" yaml:"builtin"`
	ExportPort         *ExportPortFragment         `json:"export_port,omitempty" yaml:"export_port"`
	MountHostDirectory *MountHostDirectoryFragment `json:"mount_host_directory,omitempty" yaml:"mount_host_directory"`
	Kernel             *KernelFragment             `json:"kernel,omitempty" yaml:"kernel"`
	AddVolume          *AddVolumeFragment          `json:"add_volume,omitempty" yaml:"add_volume"`
}

func (frag Fragment) Validate() error {
	if frag.RunCommand != nil {
		return frag.RunCommand.Validate()
	} else if frag.StartServiceCommand != nil {
		return frag.StartServiceCommand.Validate()
	} else if frag.AddInitScript != nil {
		return frag.AddInitScript.Validate()
	} else if frag.Environment != nil {
		return frag.Environment.Validate()
	} else if frag.RunStarlarkScript != nil {
		return frag.RunStarlarkScript.Validate()
	} else if frag.DefaultInteractive != nil {
		return frag.DefaultInteractive.Validate()
	} else if frag.LocalFile != nil {
		return frag.LocalFile.Validate()
	} else if frag.DatabaseFile != nil {
		return frag.DatabaseFile.Validate()
	} else if frag.FileContents != nil {
		return frag.FileContents.Validate()
	} else if frag.Archive != nil {
		return frag.Archive.Validate()
	} else if frag.Archive2 != nil {
		return frag.Archive2.Validate()
	} else if frag.Builtin != nil {
		return frag.Builtin.Validate()
	} else if frag.ExportPort != nil {
		return frag.ExportPort.Validate()
	} else if frag.MountHostDirectory != nil {
		return frag.MountHostDirectory.Validate()
	} else if frag.Kernel != nil {
		return frag.Kernel.Validate()
	} else if frag.AddVolume != nil {
		return frag.AddVolume.Validate()
	} else {
		return fmt.Errorf("invalid fragment: %v", frag)
	}
}

type FilesystemKind string

const (
	FilesystemKindRaw  FilesystemKind = "raw"
	FilesystemKindExt4 FilesystemKind = "ext4"
)

type Filesystem struct {
	// The kind of filesystem to create.
	Kind FilesystemKind `json:"kind" yaml:"kind"`
	// A list of Fragments to add to the Filesystem.
	Fragments []Fragment `json:"rootfs_fragments" yaml:"rootfs_fragments"`
	// The size of the rootfs in megabytes.
	StorageSize int `json:"storage_size" yaml:"storage_size"`
	// The path to persist the filesystem to. This creates a raw image and remounts it on subsequent runs.
	PersistPath string `json:"persist_path" yaml:"persist_path"`
}

func (fs Filesystem) Validate() error {
	if fs.Kind != FilesystemKindRaw && fs.Kind != FilesystemKindExt4 {
		return fmt.Errorf("invalid filesystem kind: %s", fs.Kind)
	}

	if fs.StorageSize <= 0 {
		return fmt.Errorf("invalid storage size: %d", fs.StorageSize)
	}

	for _, frag := range fs.Fragments {
		if err := frag.Validate(); err != nil {
			return fmt.Errorf("invalid fragment: %w", err)
		}
	}

	return nil
}

type InteractionKind string

const (
	InteractionSSH            InteractionKind = "ssh"
	InteractionSerial         InteractionKind = "serial"
	InteractionVNC            InteractionKind = "vnc"
	InteractionWebSSH         InteractionKind = "webssh"
	InteractionWebSSHMinimal  InteractionKind = "webssh,minimal"
	InteractionWebSSHNoBrower InteractionKind = "webssh,nobrowser"
)

const (
	CURRENT_CONFIG_VERSION = 2
)

type RelativeHostBuildDirectory struct {
	RelativePath string `json:"relative_path" yaml:"relative_path"`
}

func (cfg RelativeHostBuildDirectory) Validate() error {
	if cfg.RelativePath == "" {
		return fmt.Errorf("relative_path is required")
	}

	// the relative path mustn't be absolute
	if path.Native.IsAbs(cfg.RelativePath) {
		return fmt.Errorf("relative_path must be relative: %s", cfg.RelativePath)
	}

	return nil
}

type BuildDatabaseConfig struct {
	RelativeHostBuildDirectory *RelativeHostBuildDirectory `json:"relative_host_build_directory" yaml:"relative_host_build_directory"`
}

func (cfg BuildDatabaseConfig) Validate() error {
	if cfg.RelativeHostBuildDirectory != nil {
		return cfg.RelativeHostBuildDirectory.Validate()
	} else {
		return fmt.Errorf("invalid build database config: %v", cfg)
	}
}

// A config file that can be passed to TinyRange to configure and execute a virtual machine.
type TinyRangeConfig struct {
	// The version of the config file. This is used to determine if the config file is
	// compatible with the current version of TinyRange.
	Version int `json:"version" yaml:"version"`

	BuildDatabaseConfig []BuildDatabaseConfig `json:"build_database" yaml:"build_database"`
	// The CPU Architecture of the guest.
	Architecture CPUArchitecture `json:"architecture" yaml:"architecture"`
	// The Architecture of the root filesystem. This is a hint to enable vmm-specific optimizations.
	RootArchitecture CPUArchitecture `json:"root_architecture" yaml:"root_architecture"`
	// The kernel to boot.
	Kernel *DatabaseReference `json:"kernel" yaml:"kernel"`
	// A initramfs to pass to the kernel or nil to disable passing a initramfs.
	InitFilesystem *DatabaseReference `json:"initramfs" yaml:"initramfs"`
	// A list of filesystems to create.
	Filesystems map[string]Filesystem `json:"filesystems" yaml:"filesystems"`
	// The way the user will interact with the virtual machine (options: [ssh, serial], default: ssh).
	Interaction InteractionKind `json:"interaction" yaml:"interaction"`
	// The number of CPU cores to allocate to the virtual machine.
	CPUCores int `json:"cpu_cores" yaml:"cpu_cores"`
	// The amount of memory to allocate to the virtual machine.
	MemoryMB int `json:"memory_mb" yaml:"memory_mb"`
	// Automatically scale the CPU and RAM to the limits of the host.
	AutoScale bool `json:"auto_scale" yaml:"auto_scale"`
	// Redirect hypervisor input to the host. The VM will exit after it completes initialization.
	Debug bool `json:"debug" yaml:"debug"`
}

func (cfg TinyRangeConfig) Validate() error {
	if cfg.Version != CURRENT_CONFIG_VERSION {
		return fmt.Errorf("invalid config version: %d, expected: %d", cfg.Version, CURRENT_CONFIG_VERSION)
	}

	if cfg.Architecture == ArchInvalid {
		return fmt.Errorf("invalid architecture: %s", cfg.Architecture)
	}

	if cfg.RootArchitecture == ArchInvalid {
		return fmt.Errorf("invalid root architecture: %s", cfg.RootArchitecture)
	}

	if cfg.Interaction == "" {
		return fmt.Errorf("interaction is required")
	}

	if len(cfg.BuildDatabaseConfig) == 0 {
		return fmt.Errorf("build_database is required")
	}

	for _, db := range cfg.BuildDatabaseConfig {
		if err := db.Validate(); err != nil {
			return fmt.Errorf("invalid build database config: %w", err)
		}
	}

	for _, fs := range cfg.Filesystems {
		if err := fs.Validate(); err != nil {
			return fmt.Errorf("invalid filesystem: %w", err)
		}
	}

	if cfg.Kernel != nil {
		if err := cfg.Kernel.Validate(); err != nil {
			return fmt.Errorf("invalid kernel: %w", err)
		}
	}

	if cfg.InitFilesystem != nil {
		if err := cfg.InitFilesystem.Validate(); err != nil {
			return fmt.Errorf("invalid initramfs: %w", err)
		}
	}

	return nil
}

type BuildCacheFilesystem interface {
	// FileFromReference returns a file from a database reference.
	FileFromReference(ref DatabaseReference) (filesystem.File, error)
}

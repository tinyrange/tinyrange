package config

import (
	"fmt"
	"runtime"

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

type LocalFileFragment struct {
	HostFilename  string `json:"host_filename" yaml:"host_filename"`
	GuestFilename string `json:"guest_filename" yaml:"guest_filename"`
	Executable    bool   `json:"executable" yaml:"executable"`
}

type FileContentsFragment struct {
	Contents       []byte `json:"contents" yaml:"contents"`
	StringContents string `json:"string_contents" yaml:"string_contents"`
	GuestFilename  string `json:"guest_filename" yaml:"guest_filename"`
	Executable     bool   `json:"executable" yaml:"executable"`
}

type ArchiveFragment struct {
	HostFilename string `json:"host_filename" yaml:"host_filename"`
	Target       string `json:"target" yaml:"target"`
}

type Archive2Fragment struct {
	IndexHostFilename    string `json:"index_host_filename" yaml:"index_host_filename"`
	ContentsHostFilename string `json:"contents_host_filename" yaml:"contents_host_filename"`
	Target               string `json:"target" yaml:"target"`
}

type RunCommandFragment struct {
	Command string `json:"command" yaml:"command"`
	Raw     bool   `json:"raw" yaml:"raw"`
}

type StartServiceCommandFragment struct {
	Command string `json:"command" yaml:"command"`
}

type AddInitScriptFragment struct {
	GuestFilename string `json:"guest_filename" yaml:"guest_filename"`
}

type EnvironmentFragment struct {
	Variables []string `json:"variables" yaml:"variables"`
}

type RunStarlarkScriptFragment struct {
	Script string `json:"script" yaml:"script"`
}

type BuiltinFragment struct {
	Name          string          `json:"builtin" yaml:"builtin"`
	Architecture  CPUArchitecture `json:"architecture" yaml:"architecture"`
	GuestFilename string          `json:"guest_filename" yaml:"guest_filename"`
}

type ExportPortFragment struct {
	Name string `json:"name" yaml:"name"`
	Port int    `json:"port" yaml:"port"`
}

type DefaultInteractiveFragment struct {
	Args []string `json:"args"`
}

type MountHostDirectoryFragment struct {
	HostDirectory string `json:"host_directory" yaml:"host_directory"`
	Port          int    `json:"name" yaml:"name"`
	Writable      bool   `json:"writable" yaml:"writable"`
}

type KernelFragment struct {
	KernelFilename    string `json:"kernel_filename" yaml:"kernel_filename"`
	InitramfsFilename string `json:"initramfs_filename" yaml:"initramfs_filename"`
}

type AddVolumeFragment struct {
	VolumeName    string `json:"volume_name" yaml:"volume_name"`
	GuestPath     string `json:"guest_path" yaml:"guest_path"`
	MinimumSizeMB uint64 `json:"minimum_size_mb" yaml:"minimum_size_mb"`
	Persist       bool   `json:"persist" yaml:"persist"`
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
	FileContents       *FileContentsFragment       `json:"file_contents,omitempty" yaml:"file_contents"`
	Archive            *ArchiveFragment            `json:"archive,omitempty" yaml:"archive"`
	Archive2           *Archive2Fragment           `json:"archive2,omitempty" yaml:"archive2"`
	Builtin            *BuiltinFragment            `json:"builtin,omitempty" yaml:"builtin"`
	ExportPort         *ExportPortFragment         `json:"export_port,omitempty" yaml:"export_port"`
	MountHostDirectory *MountHostDirectoryFragment `json:"mount_host_directory,omitempty" yaml:"mount_host_directory"`
	Kernel             *KernelFragment             `json:"kernel,omitempty" yaml:"kernel"`
	AddVolume          *AddVolumeFragment          `json:"add_volume,omitempty" yaml:"add_volume"`
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

type InteractionKind string

const (
	InteractionSSH            InteractionKind = "ssh"
	InteractionSerial         InteractionKind = "serial"
	InteractionVNC            InteractionKind = "vnc"
	InteractionWebSSH         InteractionKind = "webssh"
	InteractionWebSSHMinimal  InteractionKind = "webssh,minimal"
	InteractionWebSSHNoBrower InteractionKind = "webssh,nobrowser"
)

// A config file that can be passed to TinyRange to configure and execute a virtual machine.
type TinyRangeConfig struct {
	// The base directory all other filenames resolve from.
	BaseDirectory string `json:"base_directory" yaml:"base_directory"`
	// The CPU Architecture of the guest.
	Architecture CPUArchitecture `json:"architecture" yaml:"architecture"`
	// The Architecture of the root filesystem. This is a hint to enable vmm-specific optimizations.
	RootArchitecture CPUArchitecture `json:"root_architecture" yaml:"root_architecture"`
	// The kernel to boot.
	KernelFilename string `json:"kernel_filename" yaml:"kernel_filename"`
	// A initramfs to pass to the kernel or "" to disable passing a initramfs.
	InitFilesystemFilename string `json:"init_filesystem_filename" yaml:"init_filesystem_filename"`
	// A list of filesystems to create.
	Filesystems map[string]Filesystem `json:"filesystems" yaml:"filesystems"`
	// The way the user will interact with the virtual machine (options: [ssh, serial], default: ssh).
	Interaction InteractionKind `json:"interaction" yaml:"interaction"`
	// The number of CPU cores to allocate to the virtual machine.
	CPUCores int `json:"cpu_cores" yaml:"cpu_cores"`
	// The amount of memory to allocate to the virtual machine.
	MemoryMB int `json:"memory_mb" yaml:"memory_mb"`
	// Redirect hypervisor input to the host. The VM will exit after it completes initialization.
	Debug bool `json:"debug" yaml:"debug"`
}

func (cfg TinyRangeConfig) Resolve(filename string) string {
	if filename == "" {
		return ""
	}

	// If the filename is already absolute then just use it.
	if path.Native.IsAbs(filename) {
		return filename
	}

	return path.Native.Join(cfg.BaseDirectory, filename)
}

package config

import (
	"fmt"
	"path/filepath"
	"runtime"
)

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
	Contents      []byte `json:"contents" yaml:"contents"`
	GuestFilename string `json:"guest_filename" yaml:"guest_filename"`
	Executable    bool   `json:"executable" yaml:"executable"`
}

type ArchiveFragment struct {
	HostFilename string `json:"host_filename" yaml:"host_filename"`
	Target       string `json:"target" yaml:"target"`
}

type RunCommandFragment struct {
	Command string `json:"command" yaml:"command"`
}

type EnvironmentFragment struct {
	Variables []string `json:"variables" yaml:"variables"`
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

type Fragment struct {
	// Not supported by TinyRange directly.
	RunCommand   *RunCommandFragment   `json:"run_command,omitempty" yaml:"run_command"`
	Environment  *EnvironmentFragment  `json:"environment,omitempty" yaml:"environment"`
	LocalFile    *LocalFileFragment    `json:"local_file,omitempty" yaml:"local_file"`
	FileContents *FileContentsFragment `json:"file_contents,omitempty" yaml:"file_contents"`
	Archive      *ArchiveFragment      `json:"archive,omitempty" yaml:"archive"`
	Builtin      *BuiltinFragment      `json:"builtin,omitempty" yaml:"builtin"`
	ExportPort   *ExportPortFragment   `json:"export_port,omitempty" yaml:"export_port"`
}

// A config file that can be passed to TinyRange to configure and execute a virtual machine.
type TinyRangeConfig struct {
	// The base directory all other filenames resolve from.
	BaseDirectory string `json:"base_directory" yaml:"base_directory"`
	// The CPU Architecture of the guest.
	Architecture CPUArchitecture `json:"architecture" yaml:"architecture"`
	// The filename of the hypervisor starlark script to use.
	HypervisorScript string `json:"hypervisor_script" yaml:"hypervisor_script"`
	// The kernel to boot.
	KernelFilename string `json:"kernel_filename" yaml:"kernel_filename"`
	// A initramfs to pass to the kernel or "" to disable passing a initramfs.
	InitFilesystemFilename string `json:"init_filesystem_filename" yaml:"init_filesystem_filename"`
	// A list of RootFsFragments.
	RootFsFragments []Fragment `json:"rootfs_fragments" yaml:"rootfs_fragments"`
	// The size of the rootfs in megabytes.
	StorageSize int `json:"storage_size" yaml:"storage_size"`
	// The way the user will interact with the virtual machine (options: [ssh, serial], default: ssh).
	Interaction string `json:"interaction" yaml:"interaction"`
	// The number of CPU cores to allocate to the virtual machine.
	CPUCores int `json:"cpu_cores" yaml:"cpu_cores"`
	// The amount of memory to allocate to the virtual machine.
	MemoryMB int `json:"memory_mb" yaml:"memory_mb"`
	// Config parameters to pass to the hypervisor.
	HypervisorConfig map[string]string `json:"hypervisor_config" yaml:"hypervisor_config"`
	// Redirect hypervisor input to the host. The VM will exit after it completes initialization.
	Debug bool `json:"debug" yaml:"debug"`
}

func (cfg TinyRangeConfig) Resolve(filename string) string {
	if filename == "" {
		return ""
	}

	// If the filename is already absolute then just use it.
	if filepath.IsAbs(filename) {
		return filename
	}

	return filepath.Join(cfg.BaseDirectory, filename)
}

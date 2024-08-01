package config

import (
	"path/filepath"
	"strings"
)

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

type BuiltinFragment struct {
	Name          string `json:"builtin" yaml:"builtin"`
	GuestFilename string `json:"guest_filename" yaml:"guest_filename"`
}

type Fragment struct {
	// Not supported by TinyRange directly.
	RunCommand   *RunCommandFragment   `json:"run_command,omitempty" yaml:"run_command"`
	LocalFile    *LocalFileFragment    `json:"local_file,omitempty" yaml:"local_file"`
	FileContents *FileContentsFragment `json:"file_contents,omitempty" yaml:"file_contents"`
	Archive      *ArchiveFragment      `json:"archive,omitempty" yaml:"archive"`
	Builtin      *BuiltinFragment      `json:"builtin,omitempty" yaml:"builtin"`
}

// A config file that can be passed to TinyRange to configure and execute a virtual machine.
type TinyRangeConfig struct {
	// The base directory all other filenames resolve from.
	BaseDirectory string `json:"base_directory" yaml:"base_directory"`
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
	Debug bool
}

func (cfg TinyRangeConfig) Resolve(filename string) string {
	if filename == "" {
		return ""
	}

	if strings.HasPrefix(filename, "/") {
		return filename
	}

	return filepath.Join(cfg.BaseDirectory, filename)
}

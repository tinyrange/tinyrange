package config

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

type OCIImageFragment struct {
	ImageName string `json:"image_name" yaml:"image_name"`
}

type ArchiveFragment struct {
	HostFilename string `json:"host_filename" yaml:"host_filename"`
}

type Fragment struct {
	LocalFile    *LocalFileFragment    `json:"local_file" yaml:"local_file"`
	FileContents *FileContentsFragment `json:"file_contents" yaml:"file_contents"`
	OCIImage     *OCIImageFragment     `json:"oci_image" yaml:"oci_image"`
	Archive      *ArchiveFragment      `json:"archive" yaml:"archive"`
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
	// Config parameters to pass to the hypervisor.
	HypervisorConfig map[string]string `json:"hypervisor_config" yaml:"hypervisor_config"`
}

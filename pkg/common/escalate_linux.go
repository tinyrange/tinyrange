//go:build linux

package common

import (
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/unix"
)

func EscalateToRoot() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}

	args := os.Args[1:]

	cmd := exec.Command(exe, args...)

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Unshareflags:               unix.CLONE_NEWNS | unix.CLONE_NEWUSER,
		GidMappingsEnableSetgroups: false,
		UidMappings: []syscall.SysProcIDMap{
			{
				ContainerID: 0,
				HostID:      syscall.Getuid(),
				Size:        1,
			},
		},
		GidMappings: []syscall.SysProcIDMap{
			{
				ContainerID: 0,
				HostID:      syscall.Getgid(),
				Size:        1,
			},
		},
	}

	return cmd.Run()
}

func MountTempFilesystem(path string) error {
	return unix.Mount("none", path, "tmpfs", 0, "")
}

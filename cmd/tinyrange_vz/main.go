//go:build darwin && cgo

package main

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/tinyrange/tinyrange/pkg/config"
	"github.com/tinyrange/tinyrange/pkg/linux/kernel"
	"github.com/tinyrange/tinyrange/pkg/vmm"

	"github.com/Code-Hex/vz/v3"
)

//go:embed arm64_rosetta/vmlinux
var KERNEL_ARM64_ROSETTA []byte

const ROSETTA2_SCRIPT = `
def main():
	mount("virtiofs", "rosetta2", "/rosetta", ensure_path = True)
	mount("binfmt_misc", "binfmt_misc", "/proc/sys/fs/binfmt_misc")
	file_write("/proc/sys/fs/binfmt_misc/register", ":rosetta:M:0:\\x7fELF\\x02\\x01\\x01\\x00\\x00\\x00\\x00\\x00\\x00\\x00\\x00\\x00\\x02\\x00\\x3e\\x00:\\xff\\xff\\xff\\xff\\xff\\xfe\\xfe\\x00\\xff\\xff\\xff\\xff\\xff\\xff\\xff\\xff\\xfe\\xff\\xff\\xff:/rosetta/rosetta:CF")
`

// From: https://github.com/lima-vm/lima/blob/master/pkg/vz/vm_darwin.go#L687 (Apache-2.0)
func CreateSocketPair() (*os.File, *os.File, error) {
	pairs, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_DGRAM, 0)
	if err != nil {
		return nil, nil, err
	}
	serverFD := pairs[0]
	clientFD := pairs[1]

	if err = syscall.SetsockoptInt(serverFD, syscall.SOL_SOCKET, syscall.SO_SNDBUF, 1*1024*1024); err != nil {
		return nil, nil, err
	}
	if err = syscall.SetsockoptInt(serverFD, syscall.SOL_SOCKET, syscall.SO_RCVBUF, 4*1024*1024); err != nil {
		return nil, nil, err
	}
	if err = syscall.SetsockoptInt(clientFD, syscall.SOL_SOCKET, syscall.SO_SNDBUF, 1*1024*1024); err != nil {
		return nil, nil, err
	}
	if err = syscall.SetsockoptInt(clientFD, syscall.SOL_SOCKET, syscall.SO_RCVBUF, 4*1024*1024); err != nil {
		return nil, nil, err
	}
	server := os.NewFile(uintptr(serverFD), "server")
	client := os.NewFile(uintptr(clientFD), "client")
	return server, client, nil
}

type VZVirtualMachineMonitor struct {
	config *vz.VirtualMachineConfiguration
	vm     *vz.VirtualMachine
}

func (vm *VZVirtualMachineMonitor) Run(bindOutput bool) error {
	if bindOutput {
		nullFile, err := os.Open("/dev/null")
		if err != nil {
			return fmt.Errorf("failed to create zero file: %s", err)
		}

		// console
		serialPortAttachment, err := vz.NewFileHandleSerialPortAttachment(nullFile, os.Stdout)
		if err != nil {
			return fmt.Errorf("serial port attachment creation failed: %s", err)
		}
		consoleConfig, err := vz.NewVirtioConsoleDeviceSerialPortConfiguration(serialPortAttachment)
		if err != nil {
			return fmt.Errorf("failed to create serial configuration: %s", err)
		}
		vm.config.SetSerialPortsVirtualMachineConfiguration([]*vz.VirtioConsoleDeviceSerialPortConfiguration{
			consoleConfig,
		})
	}

	var err error

	if ok, err := vm.config.Validate(); err != nil || !ok {
		return fmt.Errorf("failed to validate virtual machine configuration: %s", err)
	}

	// Create the virtual machine from the configuration.
	vm.vm, err = vz.NewVirtualMachine(vm.config)
	if err != nil {
		return fmt.Errorf("failed to create virtual machine: %s", err)
	}

	// Start the virtual machine.
	err = vm.vm.Start()
	if err != nil {
		return fmt.Errorf("failed to start virtual machine: %s", err)
	}

	for {
		select {
		case newState := <-vm.vm.StateChangedNotify():
			slog.Debug("state change", "state", newState)

			if newState == vz.VirtualMachineStateStopped {
				return nil
			}
		}
	}
}

func (m *VZVirtualMachineMonitor) Shutdown() error {
	return m.vm.Stop()
}

var (
	_ vmm.VirtualMachineMonitor = (*VZVirtualMachineMonitor)(nil)
)

func main() {
	vmm.Entry(func(driver vmm.Driver) (vmm.PrepareResult, error) {
		return vmm.PrepareResult{}, nil
	}, func(dri vmm.Driver) (vmm.VirtualMachineMonitor, error) {
		if !dri.GuestArchitecture().IsNative() {
			return nil, fmt.Errorf("vz does not support emulation")
		}

		var (
			rosetta2 bool
			err      error
		)

		if dri.RootArchitecture() == config.ArchX8664 {
			slog.Debug("Enabling Rosetta 2")

			rosetta2 = true
		}

		kern := dri.Kernel()
		if kern == nil {
			if rosetta2 {
				kern, err = dri.EnsureFile(KERNEL_ARM64_ROSETTA)
				if err != nil {
					return nil, fmt.Errorf("failed to ensure file: %w", err)
				}
			} else {
				kernelBinary, err := kernel.GetOfficialKernel(dri.GuestArchitecture())
				if err != nil {
					return nil, fmt.Errorf("failed to get official kernel: %w", err)
				}

				kern, err = dri.EnsureFile(kernelBinary)
				if err != nil {
					return nil, fmt.Errorf("failed to ensure file: %w", err)
				}
			}
		}

		kernelFilename, err := kern.HostFilename()
		if err != nil {
			return nil, fmt.Errorf("failed to get kernel filename: %s", err)
		}

		kernelCmdline := []string{"console=hvc0"}

		// Disable the default panic handler and change reboot behavior.
		kernelCmdline = append(kernelCmdline, "reboot=k", "panic=-1")

		// Set the init executable.
		kernelCmdline = append(kernelCmdline, "init=/init")

		// Set the root device. Make the root device read/write.
		kernelCmdline = append(kernelCmdline, "root=/dev/vda", "rw")

		// Trust the random number generator on the host CPU.
		kernelCmdline = append(kernelCmdline, "random.trust_cpu=on")

		// Pass the verbose flag though to the virtual machine.
		if dri.Verbose() {
			kernelCmdline = append(kernelCmdline, "tinyrange.verbose=on")
		}

		if experimental := dri.Experimental(); len(experimental) > 0 {
			kernelCmdline = append(kernelCmdline, "tinyrange.experimental="+strings.Join(experimental, ","))
		}

		kernelCmdline = append(kernelCmdline, "tinyrange.interaction="+string(dri.Interaction()))

		bootLoader, err := vz.NewLinuxBootLoader(kernelFilename, vz.WithCommandLine(
			strings.Join(kernelCmdline, " "),
		))

		cfg, err := vz.NewVirtualMachineConfiguration(
			bootLoader,
			uint(dri.CPUCores()),
			uint64(dri.MemoryMB()*1024*1024),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create virtual machine configuration: %s", err)
		}

		// entropy
		entropyConfig, err := vz.NewVirtioEntropyDeviceConfiguration()
		if err != nil {
			return nil, fmt.Errorf("entropy device creation failed: %s", err)
		}
		cfg.SetEntropyDevicesVirtualMachineConfiguration([]*vz.VirtioEntropyDeviceConfiguration{
			entropyConfig,
		})

		// block devices
		var rootDev vmm.File
		var storageDevices []vz.StorageDeviceConfiguration
		for _, disk := range dri.DiskImages() {
			if rootDev == nil {
				rootDev = disk
			}

			nbdAddr, export, err := disk.GetNBDServer(false)
			if err == nil {
				url := fmt.Sprintf("nbd://%s/%s", nbdAddr.String(), export)

				attach, err := vz.NewNetworkBlockDeviceStorageDeviceAttachment(
					url,
					1*time.Second,
					false,
					vz.DiskSynchronizationModeFull,
				)
				if err != nil {
					return nil, fmt.Errorf("failed to create storage device attachment: %s", err)
				}

				go func() {
					for {
						select {
						case <-attach.Connected():
							slog.Debug("connected to NBD server", "url", url)
						case err := <-attach.DidEncounterError():
							slog.Error("NBD server error", "url", url, "error", err)
						}
					}
				}()

				storageConfig, err := vz.NewVirtioBlockDeviceConfiguration(attach)
				if err != nil {
					return nil, fmt.Errorf("failed to create storage configuration: %s", err)
				}

				storageDevices = append(storageDevices, storageConfig)

				continue
			}

			return nil, fmt.Errorf("failed to get NBD server: %w", err)
		}
		cfg.SetStorageDevicesVirtualMachineConfiguration(storageDevices)

		// Networking
		netDev := dri.NetworkInterface()
		if netDev == nil {
			return nil, fmt.Errorf("failed to get network device: %s", err)
		}
		client, server, err := CreateSocketPair()
		if err != nil {
			return nil, fmt.Errorf("failed to create socket pair: %s", err)
		}
		err = netDev.AttachFile(server)
		if err != nil {
			return nil, fmt.Errorf("failed to attach file to user-mode networking: %s", err)
		}
		netAttach, err := vz.NewFileHandleNetworkDeviceAttachment(client)
		if err != nil {
			return nil, fmt.Errorf("failed to create network attachment: %s", err)
		}
		netConf, err := vz.NewVirtioNetworkDeviceConfiguration(netAttach)
		if err != nil {
			return nil, fmt.Errorf("failed to create network device: %s", err)
		}
		cfg.SetNetworkDevicesVirtualMachineConfiguration([]*vz.VirtioNetworkDeviceConfiguration{
			netConf,
		})
		macAddress, err := vz.NewMACAddress(netDev.MACAddress())
		if err != nil {
			return nil, fmt.Errorf("failed to make mac address: %s", err)
		}
		netConf.SetMACAddress(macAddress)

		if rosetta2 {
			availability := vz.LinuxRosettaDirectoryShareAvailability()
			if availability == vz.LinuxRosettaAvailabilityNotSupported {
				return nil, fmt.Errorf("error: Rosetta is not supported on this machine")
			} else if availability == vz.LinuxRosettaAvailabilityNotInstalled {
				slog.Info("Attempting to install Rosetta")
				err := vz.LinuxRosettaDirectoryShareInstallRosetta()
				if err != nil {
					return nil, fmt.Errorf("failed to install rosetta: %s", err)
				}
				if vz.LinuxRosettaDirectoryShareAvailability() != vz.LinuxRosettaAvailabilityInstalled {
					return nil, fmt.Errorf("failed to install rosetta: Availability != Installed")
				}
			} else if availability != vz.LinuxRosettaAvailabilityInstalled {
				return nil, fmt.Errorf("could not determine rosetta availability")
			}

			share, err := vz.NewLinuxRosettaDirectoryShare()
			if err != nil {
				return nil, fmt.Errorf("failed to create rosetta shares: %s", err)
			}

			virtioFs, err := vz.NewVirtioFileSystemDeviceConfiguration("rosetta2")
			if err != nil {
				return nil, fmt.Errorf("failed to create rosetta shares: %s", err)
			}
			virtioFs.SetDirectoryShare(share)
			cfg.SetDirectorySharingDevicesVirtualMachineConfiguration([]vz.DirectorySharingDeviceConfiguration{
				virtioFs,
			})

			// Write a script in the guest to enable Rosetta 2.
			rootFs, ok := rootDev.(vmm.Filesystem)
			if !ok {
				return nil, fmt.Errorf("root device is not a filesystem")
			}

			if err := rootFs.EnsurePath("/init.d"); err != nil {
				return nil, fmt.Errorf("failed to ensure path: %s", err)
			}

			if err := rootFs.WriteFile("/init.d/rosetta2.star", []byte(ROSETTA2_SCRIPT)); err != nil {
				return nil, fmt.Errorf("failed to write rosetta2 script: %s", err)
			}
		}

		return &VZVirtualMachineMonitor{
			config: cfg,
		}, nil
	})
}

package main

import (
	_ "embed"
	"fmt"
	"runtime"
	"strings"

	"github.com/tinyrange/tinyrange/pkg/config"
	"github.com/tinyrange/tinyrange/pkg/linux/kernel"
	"github.com/tinyrange/tinyrange/pkg/vmm"
)

//go:embed qboot.bin
var QBOOT []byte

const CFG_USE_VIRTIO_CONSOLE = true

func findQemu(driver vmm.Driver, name string) (string, error) {
	if runtime.GOOS == "windows" {
		name += ".exe"
	}

	command, err := driver.FindExecutable(name)
	if err == nil {
		return command, nil
	}

	if driver.HostOperatingSystem() == "windows" {
		command, err = driver.FindExecutable("C:\\Program Files\\qemu\\" + name)
		if err == nil {
			return command, nil
		}
	}

	return "", fmt.Errorf("%s not found", name)
}

var (
	qemuPath     = vmm.DriverFlags.String("qemu", "", "path to qemu executable")
	kernelPath   = vmm.DriverFlags.String("kernel", "", "path to linux kernel")
	cdRomPath    = vmm.DriverFlags.String("cdrom", "", "path to cdrom image")
	otherOs      = vmm.DriverFlags.Bool("other-os", false, "use other operating system (default is linux)")
	appendKernel = vmm.DriverFlags.String("append-kernel", "", "append kernel command line")
)

type OperatingSystem string

const (
	OperatingSystemLinux OperatingSystem = "linux"
	OperatingSystemOther OperatingSystem = "other"
)

func main() {
	vmm.Entry(func(driver vmm.ProxyDriver) (vmm.PrepareResult, error) {
		return vmm.PrepareResult{}, nil
	}, func(driver vmm.Driver) (vmm.VirtualMachineMonitor, error) {
		var (
			commandName string
			err         error
		)

		if *qemuPath != "" {
			commandName = *qemuPath
		} else if driver.GuestArchitecture() == config.ArchX8664 {
			commandName, err = findQemu(driver, "qemu-system-x86_64")
		} else if driver.GuestArchitecture() == config.ArchARM64 {
			commandName, err = findQemu(driver, "qemu-system-aarch64")
		} else {
			return nil, fmt.Errorf("unknown architecture: %s", driver.GuestArchitecture())
		}
		if err != nil {
			return nil, err
		}

		args := []string{
			"-nodefaults",
			"-no-user-config",
			"-nographic",
			"-no-reboot",
		}

		kernelCmdline := []string{}

		guestOs := OperatingSystemLinux
		if *cdRomPath != "" || *otherOs {
			guestOs = OperatingSystemOther
		}

		if driver.GuestArchitecture() == config.ArchARM64 {
			args = append(args, "-machine", "virt")
		}

		if driver.Accelerated() {
			// Enable hardware acceleration.
			if driver.HostOperatingSystem() == "linux" {
				args = append(args, "-enable-kvm", "-cpu", "host")
			} else if driver.HostOperatingSystem() == "darwin" {
				// Workaround for QEMU bug https://gitlab.com/qemu-project/qemu/-/issues/2665
				args = append(args, "-cpu", "cortex-a57", "-accel", "hvf")
			} else if driver.HostOperatingSystem() == "windows" {
				// I would like to use `-cpu host` here but it's broken on Windows.
				// See https://gitlab.com/qemu-project/qemu/-/issues/1594
				args = append(args, "-accel", "whpx")
			} else {
				return nil, fmt.Errorf("unsupported host operating system for hardware acceleration: %s", driver.HostOperatingSystem())
			}
		} else if driver.GuestArchitecture() == config.ArchARM64 {
			// Use the cortex-a57 CPU model.
			args = append(args, "-cpu", "cortex-a57")
		} else if driver.GuestArchitecture() == config.ArchX8664 {
			// Use the max CPU model.
			// This enables all features supported by QEMU.
			args = append(args, "-cpu", "max")
		}

		// Configure the console.
		// Use the virtio console if enabled.
		// Otherwise, use the serial console.
		if guestOs == OperatingSystemLinux && CFG_USE_VIRTIO_CONSOLE {
			args = append(args, "-device", "virtio-serial-pci,id=virtio-serial0")
			args = append(args, "-chardev", "stdio,id=charconsole0")
			args = append(args, "-device", "virtconsole,chardev=charconsole0,id=console0")
			if guestOs == OperatingSystemLinux {
				kernelCmdline = append(kernelCmdline, "console=hvc0")
			}
		} else {
			// Print a warning since the serial console degrades performance.
			driver.Logger().Warn("Using serial console")

			args = append(args, "-serial", "stdio")
			if guestOs == OperatingSystemLinux {
				kernelCmdline = append(kernelCmdline, "earlycon", "console=ttyAMA0")
			}
		}

		// Set the number of CPU cores.
		args = append(args, "-smp", fmt.Sprintf("%d", driver.CPUCores()))
		// Set the amount of memory.
		args = append(args, "-m", fmt.Sprintf("%dm", driver.MemoryMB()))

		// Add block devices using virtio-blk.
		for _, disk := range driver.DiskImages() {
			nbdSocket, export, err := disk.GetNBDServer(true)
			if err == nil {
				url := fmt.Sprintf("nbd+unix:///%s?socket=%s", export, nbdSocket.String())

				args = append(args, "-drive", fmt.Sprintf("file=%s,if=virtio,readonly=off,format=raw", url))
				continue
			}

			nbdAddr, export, err := disk.GetNBDServer(false)
			if err == nil {
				url := fmt.Sprintf("nbd://%s/%s", nbdAddr.String(), export)

				args = append(args, "-drive", fmt.Sprintf("file=%s,if=virtio,readonly=off,format=raw", url))
				continue
			}

			return nil, fmt.Errorf("failed to get NBD server: %w", err)
		}

		// Add a random number generator using virtio-rng
		args = append(args, "-device", "virtio-rng")

		// Add a network device using virtio-net.
		if netDev := driver.NetworkInterface(); netDev != nil {
			netSend, netRecv, err := netDev.GetUDPSocketPair()
			if err != nil {
				return nil, fmt.Errorf("failed to get UDP socket pair: %w", err)
			}

			macAddr := netDev.MACAddress()

			args = append(args, "-netdev", fmt.Sprintf("socket,id=net,udp=%s,localaddr=%s", netSend.String(), netRecv.String()))
			args = append(args, "-device", fmt.Sprintf("virtio-net,netdev=net,mac=%s,romfile=", macAddr.String()))
		}

		switch guestOs {
		case OperatingSystemLinux:
			// Disable the default panic handler and change reboot behavior.
			kernelCmdline = append(kernelCmdline, "reboot=k", "panic=-1")

			// Set the init executable.
			kernelCmdline = append(kernelCmdline, "init=/init")

			if initRamFs := driver.InitRamFs(); initRamFs != nil {
				// Add the initramfs. It's responsible for loading the filesystem.
				filename, err := initRamFs.HostFilename()
				if err != nil {
					return nil, fmt.Errorf("failed to get host filename: %w", err)
				}

				args = append(args, "-initrd", filename)
			} else {
				// Set the root device. Make the root device read/write.
				kernelCmdline = append(kernelCmdline, "root=/dev/vda", "rw")
			}

			// Trust the random number generator on the host CPU.
			kernelCmdline = append(kernelCmdline, "random.trust_cpu=on")

			if runtime.GOOS == "netbsd" || runtime.GOOS == "openbsd" {
				// Disable the APIC since it's not supported by NetBSD and OpenBSD.
				kernelCmdline = append(kernelCmdline, "noapic")
			}

			// Pass the verbose flag though to the virtual machine.
			if driver.Verbose() {
				kernelCmdline = append(kernelCmdline, "tinyrange.verbose=on")
			}

			if experimental := driver.Experimental(); len(experimental) > 0 {
				kernelCmdline = append(kernelCmdline, "tinyrange.experimental="+strings.Join(experimental, ","))
			}

			kernelCmdline = append(kernelCmdline, "tinyrange.interaction="+string(driver.Interaction()))

			if *kernelPath != "" {
				args = append(args, "-kernel", *kernelPath)
			} else if kern := driver.Kernel(); kern != nil {
				kernelFilename, err := kern.HostFilename()
				if err != nil {
					return nil, fmt.Errorf("failed to get host filename: %w", err)
				}

				// Add the kernel.
				args = append(args, "-kernel", kernelFilename)
			} else {
				kernel, err := kernel.LoadKernelForArchitecture(driver.GuestArchitecture())
				if err != nil {
					return nil, fmt.Errorf("failed to get official kernel: %w", err)
				}

				kernelFile, err := driver.EnsureFile(kernel)
				if err != nil {
					return nil, fmt.Errorf("failed to ensure file: %w", err)
				}

				filename, err := kernelFile.HostFilename()
				if err != nil {
					return nil, fmt.Errorf("failed to get host filename: %w", err)
				}

				args = append(args, "-kernel", filename)
			}

			if *appendKernel != "" {
				kernelCmdline = append(kernelCmdline, *appendKernel)
			}

			// Add the kernel command line.
			args = append(args, "-append", strings.Join(kernelCmdline, " "))

			if driver.GuestArchitecture() == config.ArchX8664 {
				bios, err := driver.EnsureFile(QBOOT)
				if err != nil {
					return nil, fmt.Errorf("failed to ensure file: %w", err)
				}

				filename, err := bios.HostFilename()
				if err != nil {
					return nil, fmt.Errorf("failed to get host filename: %w", err)
				}

				args = append(args, "-bios", filename)
			}
		case OperatingSystemOther:
			if *cdRomPath != "" {
				args = append(args, "-cdrom", *cdRomPath)
			}
		}

		return vmm.NewExecutable(commandName, args), nil
	})
}

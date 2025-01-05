package main

import (
	_ "embed"
	"flag"
	"fmt"
	"log/slog"
	"strings"

	"github.com/tinyrange/tinyrange/pkg/config"
	"github.com/tinyrange/tinyrange/pkg/vmm"
)

//go:embed qboot.bin
var QBOOT []byte

const CFG_USE_VIRTIO_CONSOLE = true

func findQemu(driver vmm.Driver, name string) (string, error) {
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

var qemuPath = flag.String("qemu", "", "path to qemu executable")

func main() {
	vmm.Entry(func(driver vmm.Driver) (vmm.PrepareResult, error) {
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

		if driver.GuestArchitecture() == config.ArchARM64 {
			args = append(args, "-machine", "virt")
		}

		if driver.Accelerated() {
			// Enable hardware acceleration.
			if driver.HostOperatingSystem() == "linux" {
				args = append(args, "-enable-kvm", "-cpu", "host")
			} else if driver.HostOperatingSystem() == "darwin" {
				args = append(args, "-cpu", "host", "-accel", "hvf")
			} else if driver.HostOperatingSystem() == "windows" {
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
		if CFG_USE_VIRTIO_CONSOLE {
			args = append(args, "-device", "virtio-serial-pci,id=virtio-serial0")
			args = append(args, "-chardev", "stdio,id=charconsole0")
			args = append(args, "-device", "virtconsole,chardev=charconsole0,id=console0")
			kernelCmdline = append(kernelCmdline, "console=hvc0")
		} else {
			// Print a warning since the serial console degrades performance.
			slog.Warn("Using serial console")

			args = append(args, "-serial", "stdio")
			kernelCmdline = append(kernelCmdline, "earlycon", "console=ttyAMA0")
		}

		// Set the number of CPU cores.
		args = append(args, "-smp", fmt.Sprintf("%d", driver.CPUCores()))
		// Set the amount of memory.
		args = append(args, "-m", fmt.Sprintf("%dm", driver.MemoryMB()))

		// Disable the default panic handler and change reboot behavior.
		kernelCmdline = append(kernelCmdline, "reboot=k", "panic=-1")

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

		// Pass the verbose flag though to the virtual machine.
		if driver.Verbose() {
			kernelCmdline = append(kernelCmdline, "tinyrange.verbose=on")
		}

		if experimental := driver.Experimental(); len(experimental) > 0 {
			kernelCmdline = append(kernelCmdline, "tinyrange.experimental="+strings.Join(experimental, ","))
		}

		kernelCmdline = append(kernelCmdline, "tinyrange.interaction="+string(driver.Interaction()))

		// Add a random number generator using virtio-rng
		args = append(args, "-device", "virtio-rng")

		if netDev := driver.NetworkInterface(); netDev != nil {
			netSend, netRecv, err := netDev.GetUDPSocketPair()
			if err != nil {
				return nil, fmt.Errorf("failed to get UDP socket pair: %w", err)
			}

			macAddr := netDev.MACAddress()

			args = append(args, "-netdev", fmt.Sprintf("socket,id=net,udp=%s,localaddr=%s", netSend.String(), netRecv.String()))
			args = append(args, "-device", fmt.Sprintf("virtio-net,netdev=net,mac=%s,romfile=", macAddr.String()))
		}

		kernel := driver.Kernel()
		if kernel == nil {
			return nil, fmt.Errorf("kernel not found")
		}

		kernelFilename, err := kernel.HostFilename()
		if err != nil {
			return nil, fmt.Errorf("failed to get host filename: %w", err)
		}

		// Add the kernel.
		args = append(args, "-kernel", kernelFilename)

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

		return vmm.NewExecutable(commandName, args), nil
	})
}

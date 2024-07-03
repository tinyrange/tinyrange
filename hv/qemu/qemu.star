CFG_ACCELERATE = True
CFG_CPU_CORES = 4
CFG_MEMORY_MB = 1024
CFG_USE_VIRTIO_CONSOLE = True

def main(ctx):
    args = []
    kernel_cmdline = []

    # If acceleration is enabled then enable kvm and pass the host CPU info.
    if CFG_ACCELERATE:
        args += ["-enable-kvm", "-cpu", "host"]

    # Set the command name.
    command_name = "qemu-system-x86_64"

    # Add basic flags to disable GUI display, remove defaults, and prevent reboots.
    args += [
        "-nodefaults",
        "-no-user-config",
        "-nographic",
        "-no-reboot",
    ]

    # Configure output using a serial console or virtio-console if supported.
    if CFG_USE_VIRTIO_CONSOLE:
        args += [
            "-device",
            "virtio-serial-pci,id=virtio-serial0",
            "-chardev",
            "stdio,id=charconsole0",
            "-device",
            "virtconsole,chardev=charconsole0,id=console0",
        ]
    else:
        args += [
            "-serial",
            "stdio",
        ]
        kernel_cmdline.append("console=ttyS0")

    # Set the number of CPU cores.
    args += [
        "-smp",
        "{}".format(CFG_CPU_CORES),
    ]

    # Set the amount of memory.
    args += [
        "-m",
        "{}m".format(CFG_MEMORY_MB),
    ]

    # Disable the default panic handler and change reboot behavior.
    kernel_cmdline += [
        "reboot=k",
        "panic=-1",
    ]

    # Add the root device using virtio-blk.
    args += [
        "-drive",
        "file={},if=virtio,readonly=off,format=raw".format(ctx.disk_image),
    ]

    # Set the root device and the init executable. Make the root device read/write.
    kernel_cmdline += [
        "init=/init",
        "root=/dev/vda",
        "rw",
    ]

    # Trust the random number generator on the host CPU.
    kernel_cmdline.append("random.trust_cpu=on")

    # Add a random number generator using virtio-rng
    args += [
        "-device",
        "virtio-rng",
    ]

    # Add a network adapter using virtio-net.
    args += [
        "-netdev",
        "socket,id=net,udp={},localaddr={}".format(ctx.net_send, ctx.net_recv),
        "-device",
        "virtio-net,netdev=net,mac={},romfile=".format(ctx.mac_address),
    ]

    # Set the kernel.
    args += [
        "-kernel",
        ctx.kernel,
    ]

    # Set the kerenl command line.
    args += [
        "-append",
        " ".join(kernel_cmdline),
    ]

    # Set the bios to use qbios.
    args += [
        "-bios",
        "/home/joshua/dev/projects/tinyrange2/hv/qemu/bios.bin",
    ]

    return executable(
        command = command_name,
        arguments = args,
    )

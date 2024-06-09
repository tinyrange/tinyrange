def main(ctx):
    return executable(
        command = "qemu-system-x86_64",
        arguments = [
            "-enable-kvm",
            "-nodefaults",
            "-no-user-config",
            "-nographic",
            "-serial",
            "stdio",
            "-kernel",
            "local/vmlinux_x86_64",
            "-initrd",
            "local/initramfs.cpio",
            "-append",
            "console=ttyS0 reboot=k panic=-1 init=/init",
            "-no-reboot",
        ],
    )

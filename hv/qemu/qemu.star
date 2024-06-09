def main(ctx):
    return executable(
        command = "qemu-system-x86_64",
        arguments = [
            "-enable-kvm",
            "-nographic",
            "-kernel",
            "local/vmlinux_x86_64",
            "-append",
            "console=ttyS0 reboot=k panic=-1",
            "-no-reboot",
        ],
    )

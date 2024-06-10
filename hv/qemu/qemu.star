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
            ctx.kernel,
            "-initrd",
            ctx.initrd,
            "-append",
            "console=ttyS0 reboot=k panic=-1 init=/init",
            "-no-reboot",
            "-netdev",
            "socket,id=net,udp={},localaddr={}".format(ctx.net_send, ctx.net_recv),
            "-device",
            "virtio-net,netdev=net,mac={},romfile=".format(ctx.mac_address),
        ],
    )

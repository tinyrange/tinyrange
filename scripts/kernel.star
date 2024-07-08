load_fetcher("fetchers/alpine.star")

VERSION = "6.9.8"

kernel_tgz = define.read_archive(
    define.fetch_http("https://cdn.kernel.org/pub/linux/kernel/v6.x/linux-{}.tar.xz".format(VERSION)),
    ".tar.xz",
)

builder_plan = define.plan(
    builder = "alpine@3.20",
    packages = [
        query("busybox"),
        query("busybox-binsh"),
        query("alpine-baselayout"),
        query("build-base"),
        query("flex"),
        query("bison"),
        query("linux-headers"),
        query("xz"),
        query("bash"),
    ],
    tags = ["level3"],
)

vm_settings = {
    "cpu_cores": 8,
    "memory_mb": 8192,
    "storage_size": 4096,
}

custom_kernel_env = define.build_vm(
    directives = [
        builder_plan,
        directive.archive(kernel_tgz),
        directive.run_command("(source /etc/profile;cd /linux-{};make tinyconfig)".format(VERSION)),
        directive.run_command("interactive"),
    ],
    **vm_settings
)

CONFIG_OPTIONS = [
    "CONFIG_BLOCK",
    "CONFIG_EXT4_FS",
    "CONFIG_PCI",
    "CONFIG_VIRTIO_PCI",
    "CONFIG_VIRTIO_BLK",
    "CONFIG_NET_CORE",
    "CONFIG_VIRTIO_NET",
    "CONFIG_VIRTIO_CONSOLE",
    "CONFIG_DEVTMPFS",
    "CONFIG_HW_RANDOM_VIRTIO",
]

custom_kernel_config = define.build_vm(
    directives = [
        builder_plan,
        directive.archive(kernel_tgz),
        directive.run_command("(source /etc/profile;cd /linux-{};make tinyconfig)".format(VERSION)),
    ] + [
        directive.run_command("(source /etc/profile;cd /linux-{};scripts/config --enable {})".format(VERSION, i))
        for i in CONFIG_OPTIONS
    ] + [
        directive.run_command("(source /etc/profile;cd /linux-{};make olddefconfig)".format(VERSION)),
        directive.run_command("cp /linux-{}/.config /result".format(VERSION)),
    ],
    output = "/result",
    **vm_settings
)

custom_kernel = define.build_vm(
    directives = [
        builder_plan,
        directive.archive(kernel_tgz),
        directive.add_file("/linux-{}/.config".format(VERSION), custom_kernel_config),
        directive.run_command("(source /etc/profile;cd /linux-{};make -j{})".format(VERSION, vm_settings["cpu_cores"])),
        directive.run_command("cp /linux-{}/arch/x86/boot/bzImage /result".format(VERSION)),
    ],
    output = "/result",
    **vm_settings
)

mini_plan = define.plan(
    builder = "alpine@3.20",
    packages = [
        query("busybox"),
        query("busybox-binsh"),
        query("alpine-baselayout"),
    ],
    tags = ["level3"],
)

test_vm = define.build_vm(
    kernel = custom_kernel,
    directives = [
        mini_plan,
        directive.run_command("interactive"),
    ],
    interaction = "serial",
)

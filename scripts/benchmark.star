load("scripts/alpine_kernel.star", "alpine_initramfs", "alpine_kernel", "alpine_kernel_fs", "alpine_modules_fs")

# load_fetcher("fetchers/alpine.star")

kernel_fs_320 = alpine_kernel_fs("3.20")

vm_params = {
    "initramfs": alpine_initramfs(kernel_fs_320),
    "kernel": alpine_kernel(kernel_fs_320),
    "cpu_cores": 1,
    "memory_mb": 2048,
    "storage_size": 2048,
}

vm_modfs = alpine_modules_fs(kernel_fs_320)

# Docker Configuration

docker_plan = define.plan(
    builder = "alpine@3.20",
    packages = [
        # Needs to come first since busybox provides the same commands.
        query("ifupdown-ng"),
        query("busybox"),
        query("busybox-binsh"),
        query("alpine-baselayout"),
        query("openrc"),
        query("docker"),
        query("docker-openrc"),
        query("hyperfine"),
    ],
    tags = ["level3"],
)

docker_base_directives = [
    docker_plan,
    vm_modfs,
    directive.add_file("/run/openrc/softlevel", file("")),
    directive.add_file("/etc/network/interfaces", file("")),
    directive.run_command("openrc"),
    directive.run_command("service docker start"),
    directive.run_command("while (! docker version > /dev/null 2>&1); do\nsleep 0.1\ndone"),
]

docker_test_vm = define.build_vm(
    directives = docker_base_directives + [directive.run_command("interactive")],
    **vm_params
)

# Podman Configuration

podman_plan = define.plan(
    builder = "alpine@3.20",
    packages = [
        query("busybox"),
        query("busybox-binsh"),
        query("alpine-baselayout"),
        query("openrc"),
        query("podman"),
        query("podman-openrc"),
        query("hyperfine"),
        query("ca-certificates"),
    ],
    tags = ["level3"],
)

podman_base_directives = [
    podman_plan,
    vm_modfs,
    directive.add_file("/run/openrc/softlevel", file("")),
    directive.add_file("/etc/network/interfaces", file("")),
    directive.run_command("openrc"),
    directive.run_command("service podman start"),
]

podman_test_vm = define.build_vm(
    directives = podman_base_directives + [directive.run_command("interactive")],
    **vm_params
)

# TinyRange Configuration

tinyrange_plan = define.plan(
    builder = "alpine@3.20",
    packages = [
        query("busybox"),
        query("busybox-binsh"),
        query("alpine-baselayout"),
        query("qemu-system-x86_64"),
        query("hyperfine"),
        query("ca-certificates"),
    ],
    tags = ["level3"],
)

tinyrange_base_directives = [
    tinyrange_plan,
    vm_modfs,
    directive.add_file("/root/build/tinyrange", db.get_builtin_executable("tinyrange", "x86_64")),
    directive.add_file("/root/build/init_x86_64", db.get_builtin_executable("init", "x86_64")),
    directive.add_file("/root/hv/qemu/qemu.star", db.get_builtin_executable("qemu.star", "x86_64")),
    directive.add_file("/root/hv/qemu/bios.bin", db.get_builtin_executable("qemu/bios.bin", "x86_64")),
    directive.run_command("modprobe kvm_amd || modprobe kvm_intel"),
    directive.run_command("mkdir -p /root/local/build"),
]

tinyrange_test_vm = define.build_vm(
    directives = tinyrange_base_directives + [directive.run_command("interactive")],
    **vm_params
)

# Startup Time Benchmark.

bench_startup_docker = define.build_vm(
    directives = docker_base_directives + [
        directive.run_command("docker run -it alpine whoami"),
        directive.run_command("hyperfine --export-json /result.json \"docker run -i alpine whoami\""),
    ],
    output = "/result.json",
    **vm_params
)

bench_startup_podman = define.build_vm(
    directives = podman_base_directives + [
        directive.run_command("source /etc/profile; podman run -it alpine whoami"),
        directive.run_command("source /etc/profile; hyperfine --export-json /result.json \"podman run -i alpine whoami\""),
    ],
    output = "/result.json",
    **vm_params
)

bench_startup_tinyrange = define.build_vm(
    directives = tinyrange_base_directives + [
        directive.run_command("source /etc/profile; cd /root; build/tinyrange -exec 'whoami'"),
        directive.run_command("source /etc/profile; cd /root; hyperfine --export-json /result.json \"build/tinyrange -exec 'whoami'\""),
    ],
    output = "/result.json",
    **vm_params
)

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

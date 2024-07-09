load("//lib/alpine_kernel.star", "alpine_initramfs", "alpine_kernel", "alpine_kernel_fs", "alpine_modules_fs")

# load_fetcher("//fetchers/alpine.star")

kernel_fs_320 = alpine_kernel_fs("3.20")

vm_params = {
    "initramfs": alpine_initramfs(kernel_fs_320),
    "kernel": alpine_kernel(kernel_fs_320),
    "cpu_cores": 1,
    "memory_mb": 2048,
    "storage_size": 2048,
}

vm_modfs = alpine_modules_fs(kernel_fs_320)

def make_vm(directives):
    return define.build_vm(
        directives = directives + [directive.run_command("interactive")],
        **vm_params
    )

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

docker_test_vm = make_vm(docker_base_directives)

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

podman_test_vm = make_vm(podman_base_directives)

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
    directive.add_file("/root/tinyrange", db.get_builtin_executable("tinyrange", "x86_64")),
    directive.add_file("/root/tinyrange_qemu.star", db.get_builtin_executable("tinyrange_qemu.star", "x86_64")),
    directive.run_command("modprobe kvm_amd || modprobe kvm_intel"),
    directive.run_command("mkdir -p /root/local/build"),
]

tinyrange_test_vm = make_vm(tinyrange_base_directives)

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
        directive.run_command("source /etc/profile; cd /root; ./tinyrange -exec 'whoami'"),
        directive.run_command("source /etc/profile; cd /root; hyperfine --export-json /result.json \"./tinyrange -exec 'whoami'\""),
    ],
    output = "/result.json",
    **vm_params
)

# bench_startup = define.group(
#     bench_startup_docker,
#     bench_startup_podman,
#     bench_startup_tinyrange,
# )

def main(args):
    output = args.output()

    arr = []
    arr += [bench_startup_docker] * 100
    arr += [bench_startup_podman] * 100
    arr += [bench_startup_tinyrange] * 100

    arr = shuffle(arr)

    for bench in arr:
        res = db.build(bench, always_rebuild = True)
        out = json.decode(res.read())
        print(out)
        output.write("\t".join([str(time()), json.encode(out)]) + "\n")
        # sleep(duration("1m"))

load("//lib/alpine_kernel.star", "alpine_initramfs", "alpine_kernel", "alpine_kernel_fs", "alpine_modules_fs")

# load_fetcher("//fetchers/alpine.star")

kernel_fs_320 = alpine_kernel_fs("3.20")

vm_params = {
    "initramfs": alpine_initramfs(kernel_fs_320),
    "kernel": alpine_kernel(kernel_fs_320),
    "cpu_cores": 1,
    "memory_mb": 4096,
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
    directive.builtin("tinyrange", "/root/tinyrange"),
    directive.builtin("tinyrange_qemu.star", "/root/tinyrange_qemu.star"),
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
        directive.run_command("source /etc/profile; cd /root; ./tinyrange login -E whoami"),
        directive.run_command("source /etc/profile; cd /root; hyperfine --export-json /result.json \"./tinyrange login -E whoami\""),
    ],
    output = "/result.json",
    **vm_params
)

fio_args = "--randrepeat=1 --ioengine=libaio --direct=1 --name=test --filename=test --bs=4k --size=1G --readwrite=readwrite --ramp_time=4"

bench_cpu_docker = define.build_vm(
    directives = docker_base_directives + [
        directive.run_command("docker run --rm zyclonite/sysbench cpu run > /output.txt"),
    ],
    output = "/output.txt",
    **vm_params
)

bench_memory_docker = define.build_vm(
    directives = docker_base_directives + [
        directive.run_command("docker run --rm zyclonite/sysbench memory run > /output.txt"),
    ],
    output = "/output.txt",
    **vm_params
)

bench_fileio_docker = define.build_vm(
    directives = docker_base_directives + [
        directive.run_command("docker run --rm xridge/fio " + fio_args + " > /output.txt"),
    ],
    output = "/output.txt",
    **vm_params
)

bench_cpu_podman = define.build_vm(
    directives = podman_base_directives + [
        directive.run_command("source /etc/profile; podman run --rm zyclonite/sysbench cpu run > /output.txt"),
    ],
    output = "/output.txt",
    **vm_params
)

bench_memory_podman = define.build_vm(
    directives = podman_base_directives + [
        directive.run_command("source /etc/profile; podman run --rm zyclonite/sysbench memory run > /output.txt"),
    ],
    output = "/output.txt",
    **vm_params
)

bench_fileio_podman = define.build_vm(
    directives = podman_base_directives + [
        directive.run_command("source /etc/profile; podman run --rm xridge/fio " + fio_args + " > /output.txt"),
    ],
    output = "/output.txt",
    **vm_params
)

bench_cpu_tinyrange = define.build_vm(
    directives = tinyrange_base_directives + [
        directive.run_command("source /etc/profile; cd /root; ./tinyrange login sysbench -E \"sysbench cpu run\" > /output.txt"),
    ],
    output = "/output.txt",
    **vm_params
)

bench_memory_tinyrange = define.build_vm(
    directives = tinyrange_base_directives + [
        directive.run_command("source /etc/profile; cd /root; ./tinyrange login sysbench -E \"sysbench memory run\" > /output.txt"),
    ],
    output = "/output.txt",
    **vm_params
)

bench_fileio_tinyrange = define.build_vm(
    directives = tinyrange_base_directives + [
        directive.run_command("source /etc/profile; cd /root; ./tinyrange login --storage 2048 fio -E \"fio " + fio_args + "\" > /output.txt"),
    ],
    output = "/output.txt",
    **vm_params
)

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

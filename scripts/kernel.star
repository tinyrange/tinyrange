load_fetcher("fetchers/alpine.star")

INIT_STAR = """
def main():
    # Read the list of modules to load.
    load_modules = json.decode(file_read("/.kernel/load_list.json"))

    # Load each module.
    for name in load_modules:
        insmod(file_read(name))

    # Mount the rootfs.
    mount("devtmpfs", "devtmpfs", "/dev", ensure_path = True, ignore_error = True)
    mount("ext4", "/dev/vda", "/root", ensure_path=True)

    # Change into the rootfs.
    chroot("/root")
    chdir("/")

    # Exec the real init.
    exec("/init")
"""

ALPINE_ROOTFS = define.read_archive(
    define.fetch_http("https://dl-cdn.alpinelinux.org/alpine/v3.20/releases/x86_64/alpine-minirootfs-3.20.1-x86_64.tar.gz"),
    ".tar.gz",
)

GO_DOWNLOAD = define.read_archive(
    define.fetch_http("https://go.dev/dl/go1.22.5.linux-amd64.tar.gz"),
    ".tar.gz",
)

TINYRANGE_SRC = define.read_archive(
    define.fetch_http("https://github.com/tinyrange/tinyrange/archive/refs/heads/alpha.tar.gz"),
    ".tar.gz",
)

KERNEL = define.fetch_http("https://github.com/tinyrange/linux_build/releases/download/linux_x86_6.6.7/vmlinux_x86_64")

def build_go(input, package, os = "", arch = ""):
    build_script = """#!/bin/sh
PATH=$PATH:/usr/local/go/bin
export GOROOT=/usr/local/go
export CGO_ENABLED=0
export GOOS={}
export GOARCH={}

# Perform the build.
(cd /input/*;go build -o /result {})
""".format(os, arch, package)

    return define.build_vm(
        directives = [
            ALPINE_ROOTFS,
            directive.archive(GO_DOWNLOAD, target = "/usr/local"),
            directive.archive(input, target = "/input"),
            directive.add_file("/build.sh", file(build_script, executable = True)),
            directive.run_command("/build.sh"),
        ],
        output = "/result",
    )

def get_kernel(ctx, plan):
    fs = plan.filesystem()

    for ent in fs["boot"]:
        if ent.base.startswith("vmlinuz-"):
            return ent

    return error("kernel not found")

def get_modules_archive(ctx, plan):
    fs = plan.filesystem()

    out = filesystem()

    out["lib/modules"] = fs["lib/modules"]

    return ctx.archive(out)

ESSENTIAL_MODULES = [
    "kernel/drivers/block/virtio_blk.ko.gz",  # Needed to mount module filesystem.
    "kernel/drivers/virtio/virtio_pci.ko.gz",  # Needed on x86_64 to find devices.
    "kernel/drivers/net/virtio_net.ko.gz",  # Network Interface driver.
    "kernel/net/packet/af_packet.ko.gz",  # Needed for DHCP
    "kernel/drivers/char/hw_random/virtio-rng.ko.gz",  # Speeds up random number generation.
    "kernel/crypto/crc32c_generic.ko.gz",  # Needed for ext4.
    "kernel/fs/ext4/ext4.ko.gz",  # Needed for preinit and new_boot.
]

def parse_modules_dep(contents):
    ret = {}

    for line in contents.splitlines():
        k, _, v = line.partition(": ")
        if len(v) > 0:
            v = v.split(" ")
            ret[k] = reversed(v)

    return ret

def get_essential_modules(lst, deps):
    ret = []

    for k in lst:
        if k in deps:
            ret += get_essential_modules(deps[k], deps)

        ret.append(k)

    return [i for i in set(ret)]

def get_inital_modules(ctx, plan):
    fs = plan.filesystem()

    module_dir = [f for f in fs["lib/modules"]][0]

    deps = parse_modules_dep(module_dir["modules.dep"].read())

    essential_modules = get_essential_modules(ESSENTIAL_MODULES, deps)

    out = filesystem()

    load_names = []

    for name in essential_modules:
        if name in module_dir:
            f = module_dir[name]
            filename = ".kernel/modules/{}".format(f.base.removesuffix(".gz"))
            out[filename] = file(f.read_compressed(".gz"))
            load_names.append(filename)

    out[".kernel/load_list.json"] = json.encode(load_names)

    return ctx.archive(out)

def main(args):
    kernel_fs = db.build(define.plan(
        builder = "alpine@3.20",
        packages = [
            query("linux-virt"),
        ],
        tags = ["download"],
    ))

    fs = define.build_fs(
        directives = [
            define.build(get_inital_modules, kernel_fs),
            directive.add_file("/init", args["init"], executable = True),
            directive.add_file("/init.star", file(INIT_STAR)),
        ],
        kind = "initramfs",
    )

    vm = define.build_vm(
        kernel = define.build(get_kernel, kernel_fs),
        directives = [
            define.plan(
                builder = "alpine@3.20",
                packages = [
                    query("busybox"),
                    query("busybox-binsh"),
                    query("alpine-baselayout"),
                ],
                tags = ["level3"],
            ),
            define.build(get_modules_archive, kernel_fs),
            directive.run_command("interactive"),
        ],
        initramfs = fs,
        # interaction = "serial",
    )

    db.build(vm, always_rebuild = True)

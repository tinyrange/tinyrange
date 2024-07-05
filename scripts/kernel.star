INIT_STAR = """
def main():
    print("Hello, World")
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

def main(args):
    init = build_go(
        TINYRANGE_SRC,
        "github.com/tinyrange/tinyrange/cmd/init",
        os = "linux",
        arch = "amd64",
    )

    init_file = db.build(init)

    fs = define.build_fs(
        directives = [
            directive.add_file("/init", init_file, executable = True),
            directive.add_file("/init.star", file(INIT_STAR)),
        ],
        kind = "initramfs",
    )

    vm = define.build_vm(
        kernel = KERNEL,
        initramfs = fs,
        interaction = "serial",
    )

    db.build(vm, always_rebuild = True)

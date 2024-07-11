# TinyRange

TinyRange is a light-weight scriptable orchestration system for building and running virtual machines with a focus on speed and flexibility for development.

TinyRange is currently a Pre-Alpha and expect major breaking changes as the architecture is improved and features are explored.

## Getting Started

Currently TinyRange only runs on Linux x86_64 and Windows x86_64 but support for other operating systems (MacOS, BSDs) and architectures (ARM64, RISC-V) is on the roadmap.

The easiest way to get started is to grab the latest executable in https://github.com/tinyrange/tinyrange/releases. Just run the installer. You'll need the installer and `tinyrange_qemu.star` in the same directory.

Once TinyRange is installed you can start it with `tinyrange` (it's installed to `%HOME%/bin` on Windows and `~/.local/bin` on Linux).

## Building from Source

TinyRange is written in [Go](https://go.dev/) and requires both Go and [QEMU](https://www.qemu.org/) to be installed before it can be built.

It can be built and run with the following code.

```sh
git clone https://github.com/tinyrange/tinyrange
cd tinyrange
./build.go && ./build/pkg2 -script scripts/tinyrange.star
```

## Scripting

```py
load_fetcher("fetchers/alpine.star")

def main(args):
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
        directive.run_command("interactive"),
    ]

    # Run the virtual machine using TinyRange.
    # The final run_command makes it interactive.
    db.build(
        define.build_vm(
            directives = directives,
        ),
        always_rebuild = True,
    )
```

The scripting in TinyRange is built around making build definitions which are built with `db.build`. Here we are using two definitions `define.build_vm` and `define.plan`.

- `define.plan` creates a list of directives containing archives and commands to be used in a virtual machine.
- `define.build_vm` runs a virtual machine with a list of directives, it can optionally specify a output file which will be copied from the VM as the build result.

One easy change here is adding additional `query` lines to install packages inside the virtual machine. Try adding `query("build-base")` to get a C and C++ compiler or `query("go")` to get a Go compiler. These packages names come from [Alpine Linux](https://www.alpinelinux.org/).

## Rebuilding `pkg/filesystem/ext4/ext4_gen.go`

```sh
go install github.com/tinyrange/vm/cmd/structgen
structgen -input pkg/filesystem/ext4/ext4.struct -output pkg/filesystem/ext4/ext4_gen.go -package ext4
```

## Videos implementing TinyRange

- Part 1: https://www.youtube.com/watch?v=W5OwOUV9iAQ
- Part 2: https://www.youtube.com/watch?v=tTTcN2kflFM
- Part 3: https://www.youtube.com/watch?v=3d-4S2oaDfw
- Part 4: https://www.youtube.com/watch?v=HKvnG4SOpzo
- Part 5: https://www.youtube.com/watch?v=nEC2dUQHLnc

I'll publish another video walking though the configuration syntax and networking code at some point in the future.
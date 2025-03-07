# TinyRange

![Alt](https://repobeats.axiom.co/api/embed/5e4b724db82dc0b0fb2c39977c6f7c9ac39447d5.svg "Repobeats analytics image")

TinyRange is a light-weight scriptable orchestration system for building and running virtual machines with a focus on speed and flexibility for development.

TinyRange is currently a Pre-Alpha and expect major breaking changes as the architecture is improved and features are explored.



## Getting Started

Currently TinyRange runs on Windows (amd64), MacOS (arm64), and Linux (amd64, arm64).

Binaries can be downloaded from the releases tab [https://github.com/tinyrange/tinyrange/releases](https://github.com/tinyrange/tinyrange/releases).

The latest release can also be installed in one command on MacOS and Linux with...

```shell
curl -sSL https://j5.nz/install_tinyrange.sh | bash
```

TinyRange can also be installed via PyPI with...

```shell
pip install tinyrange-sys
```

**Note:** TinyRange can also run on BSD (OpenBSD, FreeBSD, NetBSD) (amd64), and Solaris (OmniOS) (amd64) but binaries are not tested and acceleration is not currently supported.

## Building from Source

TinyRange is written in [Go](https://go.dev/) and requires Go to be installed before it can be built.

`./tools/build.go` will automatically download a copy of QEMU if you are on Windows (amd64), MacOS (arm64), or Linux (amd64, arm64).

TinyRange can be built and run from source with the following code.

```sh
git clone https://github.com/tinyrange/tinyrange
cd tinyrange
./tools/build.go -run -- login
```

## What is TinyRange

![TinyRange Architecture Diagram](docs/archDiagram.svg)

TinyRange is an alternative to containers and virtual machines sitting somewhere in between the two in functionality and tradeoffs.

## Rebuilding `pkg/filesystem/ext4/ext4_gen.go`

```sh
go install github.com/tinyrange/vm/cmd/structgen
structgen -input pkg/filesystem/ext4/ext4.struct -output pkg/filesystem/ext4/ext4_gen.go -package ext4
```

## Videos implementing TinyRange

- Part 1: [https://www.youtube.com/watch?v=W5OwOUV9iAQ](https://www.youtube.com/watch?v=W5OwOUV9iAQ)
- Part 2: [https://www.youtube.com/watch?v=tTTcN2kflFM](https://www.youtube.com/watch?v=tTTcN2kflFM)
- Part 3: [https://www.youtube.com/watch?v=3d-4S2oaDfw](https://www.youtube.com/watch?v=3d-4S2oaDfw)
- Part 4: [https://www.youtube.com/watch?v=HKvnG4SOpzo](https://www.youtube.com/watch?v=HKvnG4SOpzo)
- Part 5: [https://www.youtube.com/watch?v=nEC2dUQHLnc](https://www.youtube.com/watch?v=nEC2dUQHLnc)

I'll publish another video walking though the configuration syntax and networking code at some point in the future.

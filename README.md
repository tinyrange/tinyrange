# TinyRange

TinyRange is a light-weight scriptable orchestration system for building and running virtual machines with a focus on speed and flexibility for development over production reliability.

## Getting Started

Currently TinyRange only runs on Linux x86_64 but support for other operating systems (Windows, MacOS, BSDs) and architectures (ARM64, RISC-V) is on the roadmap.

```sh
./build.sh && ./build/pkg2 -script scripts/tinyrange.star
```

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
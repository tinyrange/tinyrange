# TinyRange

# Plan for implementation.

**Work in Progress**

This branch tracks a public rewrite of TinyRange documented through the medium of live streams.

1. [x] Get a virtual machine started using QEMU and outputting to the screen.
2. [x] Get the virtual machine booting an initramfs provided by TinyRange (starting with Alpine Linux).
3. [x] Write an init system for the virtual machine in Go and replace the existing initramfs.
4. [x] Connect networking from the host to the guest using gVisor netstack.
5. [x] Connect an ext4 filesystem exposed as a NBD(Network Block Device) from the host.
6. [x] Boot a full Linux distribution from a OCI image.
7. [x] Write a DNS server in the host for the virtual guest.
8. [x] Switch from using stdout from QEMU to SSH from the host.
9. [x] Add support for connecting to arbitrary websites in the guest.
10. [x] Make the virtual machine customizable. Including customizable scripts.

## Videos

- Part 1 (item 1 and 2): https://www.youtube.com/watch?v=W5OwOUV9iAQ
- Part 2 (item 3 and 4): https://www.youtube.com/watch?v=tTTcN2kflFM
- Part 3 (item 5): https://www.youtube.com/watch?v=3d-4S2oaDfw
- Part 4 (item 6): https://www.youtube.com/watch?v=HKvnG4SOpzo
- Part 5 (item 7 and 8): https://www.youtube.com/watch?v=nEC2dUQHLnc

I've completed part 9 and part 10 and I'll edit and publish a separate video for it.

# Getting Started

```sh
./build.sh
./build/pkg2 -script scripts/tinyrange.star
```

# Rebuilding `pkg/filesystem/ext4/ext4_gen.go`

```sh
go install github.com/tinyrange/vm/cmd/structgen
structgen -input pkg/filesystem/ext4/ext4.struct -output pkg/filesystem/ext4/ext4_gen.go -package ext4
```

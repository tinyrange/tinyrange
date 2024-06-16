# TinyRange

# Plan for implementation.

**Work in Progress**

This branch tracks a public rewrite of TinyRange documented through the medium of live streams.

1. [x] Get a virtual machine started using QEMU and outputting to the screen.
2. [x] Get the virtual machine booting an initramfs provided by TinyRange (starting with Alpine Linux).
3. [x] Write an init system for the virtual machine in Go and replace the existing initramfs.
4. [x] Connect networking from the host to the guest using gVisor netstack.
5. [x] Connect an ext4 filesystem exposed as a NBD(Network Block Device) from the host.
6. [ ] Boot a full Linux distribution from a OCI image.
7. [ ] Write a DHCP and DNS server in the host for the virtual guest.
8. [ ] Switch from using stdout from QEMU to SSH from the host.
9. [ ] Write a network caching layer for the virtual guest so it can download packages.
10. [ ] Add support for connecting to arbitrary websites in the guest.
11. [ ] Make the virtual machine customizable. Including customizable scripts.

## Videos

- Part 1 (item 1 and 2): https://www.youtube.com/watch?v=W5OwOUV9iAQ
- Part 2 (item 3 and 4): https://www.youtube.com/watch?v=tTTcN2kflFM
- Part 3 (item 5): https://www.youtube.com/watch?v=3d-4S2oaDfw

# Getting Started

```sh
mkdir -p local
(cd local; wget https://github.com/tinyrange/linux_build/releases/download/linux_x86_6.6.7/vmlinux_x86_64)
(cd local; wget https://dl-cdn.alpinelinux.org/alpine/v3.20/releases/x86_64/alpine-minirootfs-3.20.0-x86_64.tar.gz)
./build.sh
./build/tinyrange
```

# Rebuilding `pkg/filesystem/ext4/ext4_gen.go`

```sh
go install github.com/tinyrange/vm/cmd/structgen
structgen -input pkg/filesystem/ext4/ext4.struct -output pkg/filesystem/ext4/ext4_gen.go -package ext4
```
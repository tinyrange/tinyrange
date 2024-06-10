# TinyRange

# Plan for implementation.

**Work in Progress**

This branch tracks a public rewrite of TinyRange documented through the medium of live streams.

1. [x] Get a virtual machine started using QEMU and outputting to the screen.
2. [x] Get the virtual machine booting an initramfs provided by TinyRange (starting with Alpine Linux).
3. [x] Write an init system for the virtual machine in Go and replace the existing initramfs.
4. [x] Connect networking from the host to the guest using gVisor netstack.
5. [ ] Write a DHCP and DNS server in the host for the virtual guest.
6. [ ] Switch from using stdout from QEMU to SSH from the host.
7. [ ] Connect an ext4 filesystem exposed as a NBD(Network Block Device) from the host.
8. [ ] Boot a full Linux distribution from a OCI image.
9. [ ] Write a network caching layer for the virtual guest so it can download packages.
10. [ ] Add support for connecting to arbitrary websites in the guest.
11. [ ] Make the virtual machine customizable. Including customizable scripts.

## Videos

- Part 1 (item 1 and 2): https://www.youtube.com/watch?v=W5OwOUV9iAQ
- Part 2 (item 3 and 4): https://www.youtube.com/watch?v=tTTcN2kflFM

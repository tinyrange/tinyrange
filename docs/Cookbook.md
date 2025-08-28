# I want to...

## Create a new VM with a file from...

### My host machine

```sh
tinyrange login -f filename:/path/in/guest
```

### The internet

```sh
tinyrange login -f https://example.com/file:/path/in/guest
```

## Extract an archive into a new VM from...

### My host machine

```sh
tinyrange login -a filename.zip:/guest/directory
tinyrange login -a filename.tar.gz:/guest/directory
tinyrange login -a filename.tar.zst:/guest/directory
tinyrange login -a filename.tar.xz:/guest/directory
tinyrange login -a filename.cpio:/guest/directory
tinyrange login -a filename.ar:/guest/directory
tinyrange login -a filename.archive:/guest/directory
```

### The internet

```sh
tinyrange login -a https://example.com/filename.zip:/guest/directory
tinyrange login -a https://example.com/filename.tar.gz:/guest/directory
tinyrange login -a https://example.com/filename.tar.zst:/guest/directory
tinyrange login -a https://example.com/filename.tar.xz:/guest/directory
tinyrange login -a https://example.com/filename.cpio:/guest/directory
tinyrange login -a https://example.com/filename.ar:/guest/directory
tinyrange login -a https://example.com/filename.archive:/guest/directory
```

## Share a directory...

### Read-only

```sh
tinyrange login --mount local/test:/mnt
```

### Read-Write

```sh
tinyrange login --mount-rw local/test:/mnt
```

## Mount a...

### Temporary Volume

```sh
tinyrange login --volume test,1g,/mnt
```

### Persistent Volume

```sh
tinyrange login --volume test,1g,/mnt,persist
```

## Adjust the...

### CPU Core Count

```sh
tinyrange login --cpu 4
```

### Memory Amount

```sh
tinyrange login --ram 4096
```

### Storage Amount

```sh
tinyrange login --storage 4096
```

## Install...

### A C compiler

```sh
tinyrange login build-base
```

### Python3

```sh
tinyrange login python3
```

### Golang...

#### From Alpine Repositories

```sh
tinyrange login go
```

### Rust (via Cargo)

```sh
tinyrange login cargo
```

## Create a VM from a OCI container

```sh
tinyrange login --oci ubuntu
```
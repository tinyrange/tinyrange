# I want to...

## Create a new VM with a file from...

### My host machine

```sh
tinyrange login -f filename
```

### The internet

```sh
tinyrange login -f https://example.com/file
```

## Extract an archive into a new VM from...

### My host machine

```sh
tinyrange login -a filename.zip
tinyrange login -a filename.tar.gz
tinyrange login -a filename.tar.zst
tinyrange login -a filename.tar.xz
tinyrange login -a filename.cpio
tinyrange login -a filename.ar
tinyrange login -a filename.archive
```

### The internet

```sh
tinyrange login -a https://example.com/filename.zip
tinyrange login -a https://example.com/filename.tar.gz
tinyrange login -a https://example.com/filename.tar.zst
tinyrange login -a https://example.com/filename.tar.xz
tinyrange login -a https://example.com/filename.cpio
tinyrange login -a https://example.com/filename.ar
tinyrange login -a https://example.com/filename.archive
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
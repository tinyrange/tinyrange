#!/bin/bash

set -ex

# NOTE: TinyRange does not currently support all these architectures but there is hope to support them in the future.

# Tier 1: Fully Supported
go run build.go -release -os linux -arch amd64
go run build.go -release -os windows -arch amd64
go run build.go -release -os darwin -arch arm64
go run build.go -release -os linux -arch arm64

# Tier 2: Ad-hoc Support
go run build.go -release -os linux -arch riscv64
go run build.go -release -os darwin -arch amd64
go run build.go -release -os illumos -arch amd64
go run build.go -release -os freebsd -arch amd64
go run build.go -release -os openbsd -arch amd64
go run build.go -release -os netbsd -arch amd64

# Tier 3: Currently Unsupported
go run build.go -release -os windows -arch arm64
go run build.go -release -os wasip1 -arch wasm

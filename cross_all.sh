#!/bin/bash

set -ex

# NOTE: TinyRange does not currently support all these architectures but there is hope to support them in the future.

# Tier 1: Fully Supported
go run build.go -os linux -arch amd64
go run build.go -os windows -arch amd64
go run build.go -os darwin -arch arm64
go run build.go -os linux -arch arm64
        
# Tier 2: Ad-hoc Support
go run build.go -os linux -arch riscv64
go run build.go -os darwin -arch amd64
go run build.go -os illumos -arch amd64
go run build.go -os freebsd -arch amd64
go run build.go -os openbsd -arch amd64
go run build.go -os netbsd -arch amd64

# Tier 3: Currently Unsupported
go run build.go -os windows -arch arm64
go run build.go -os wasip1 -arch wasm

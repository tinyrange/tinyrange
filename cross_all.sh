#!/bin/bash

set -ex

# NOTE: TinyRange does not currently support all these architectures but there is hope to support them in the future.

# Tier 1: Fully Supported
go run tools/build.go -os linux -arch amd64 -release
go run tools/build.go -os windows -arch amd64 -release
go run tools/build.go -os darwin -arch arm64 -release
go run tools/build.go -os linux -arch arm64 -release

# Tier 2: Ad-hoc Support
go run tools/build.go -os darwin -arch amd64 -release
go run tools/build.go -os freebsd -arch amd64 -release
go run tools/build.go -os openbsd -arch amd64 -release

# Tier 3: Currently Unsupported

# Tier 4: Broken
# go run tools/build.go -os windows -arch arm64 -release
# go run tools/build.go -os linux -arch riscv64 -release
# go run tools/build.go -os illumos -arch amd64 -release
# go run tools/build.go -os netbsd -arch amd64 -release
# go run tools/build.go -os wasip1 -arch wasm -release

#!/bin/bash

set -ex

export GOOS=linux
export GOARCH=amd64
export CGO_ENABLED=0

go build -o build/init_x86_64 github.com/tinyrange/tinyrange/cmd/init
go build -o build/tinyrange github.com/tinyrange/tinyrange/cmd/tinyrange
go build -o build/pkg2 github.com/tinyrange/tinyrange/cmd/pkg2
go build -o build/builder github.com/tinyrange/tinyrange/cmd/builder

#!/bin/bash

set -ex

export GOOS=linux
export GOARCH=amd64
export CGO_ENABLED=0

go build -o build/pkg2 github.com/tinyrange/pkg2/v2
go build -o build/builder github.com/tinyrange/pkg2/v2/cmd/builder
#!/bin/bash

set -ex

export CGO_ENABLED=0

GOOS=linux go build -o pkg/init/init github.com/tinyrange/tinyrange/cmd/init
go build -o build/tinyrange github.com/tinyrange/tinyrange/cmd/tinyrange
go build -o build/pkg2 github.com/tinyrange/tinyrange/cmd/pkg2

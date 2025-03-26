#!/usr/bin/env bash

set -ex

./tools/build.go

TEMPLATE=$(
    ./build/tinyrange \
        login \
        --experimental ext4_resize \
        --storage 1024 \
        --template
)

./build/tinyrange_qemu -experimental ext4_resize -exportfs local/filesystem.ext4 $TEMPLATE

cp local/filesystem.ext4 local/filesystem.before

fsck.ext4 -f local/filesystem.ext4 -y || true

./tools/build.go -exp hexDiff -- local/filesystem.before local/filesystem.ext4

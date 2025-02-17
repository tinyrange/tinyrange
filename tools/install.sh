#!/usr/bin/env bash

set -e
set -o pipefail

GITHUB_REPO="tinyrange/tinyrange"
DEST_DIR=~/bin

# Check to make sure we have the unzip command.
if ! command -v unzip &>/dev/null; then
    echo "unzip command not found. Please install unzip."
    exit 1
fi

# Check to see if TinyRange is already installed in DEST_DIR and ask the user if they want to overwrite it.
if [ -f $DEST_DIR/tinyrange ]; then
    echo "TinyRange is already installed in $DEST_DIR."
    read -p "Do you want to overwrite it? [y/N] " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        echo "Exiting..."
        exit 0
    fi
fi

get_latest_release() {
    curl --silent "https://api.github.com/repos/$1/releases/latest" | # Get latest release from GitHub api
        grep '"tag_name":' |                                          # Get tag line
        sed -E 's/.*"([^"]+)".*/\1/'                                  # Pluck JSON value
}

LATEST_VERSION=$(get_latest_release $GITHUB_REPO)

# Figure out the platform.
UNAME=$(uname)
ARCH=$(uname -m)

# Switch on uname.
case "$UNAME/$ARCH" in
"Linux/x86_64")
    PLATFORM="linux-amd64"
    ;;
"Linux/aarch64")
    PLATFORM="linux-arm64"
    ;;
"Darwin/arm64")
    PLATFORM="darwin-arm64"
    ;;
*)
    echo "TinyRange does not include pre-built binaries for $UNAME."
    echo "You can try building TinyRange from scratch following the instructions at https://github.com/tinyrange/tinyrange."
    exit 1
    ;;
esac

echo "Downloading TinyRange $LATEST_VERSION for $PLATFORM..."

# Download the release to a temporary directory.
TMP_DIR=$(mktemp -d)
(
    cd $TMP_DIR
    curl -L "https://github.com/tinyrange/tinyrange/releases/download/$LATEST_VERSION/tinyrange-$PLATFORM.zip" -o tinyrange.zip
)

echo "Installing TinyRange $LATEST_VERSION to $DEST_DIR..."

# Ensure that the destination directory exists.
mkdir -p $DEST_DIR

# Unzip the release and move it to the destination directory.
# Strip the top-level directory from the zip file.
unzip -q $TMP_DIR/tinyrange.zip -d $TMP_DIR
mv $TMP_DIR/tinyrange/* $DEST_DIR
rm -rf $TMP_DIR

# Remove the portable flag.
rm $DEST_DIR/tinyrange.portable

echo "TinyRange $LATEST_VERSION has been installed to $DEST_DIR."

#!/usr/bin/env bash

# Isolate Go environment to allow Codex sandbox to work autonomously
# without interfering with the host system's Go setup.

set -euo pipefail

# Isolate all files go writes to the local directory
export GOMODCACHE="${PWD}/local/gomodcache"
export GOCACHE="${PWD}/local/gocache"
export GOPATH="${PWD}/local/gopath"
export TMPDIR="${PWD}/local/tmp"

mkdir -p "$GOMODCACHE" "$GOCACHE" "$GOPATH" "$TMPDIR"

exec go "$@"
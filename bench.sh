#!/bin/bash

set -ex

./build.go

# build/tinyrange build -o benchmarks/bench_startup_docker.md scripts/benchmark.star:bench_startup_docker
# build/tinyrange build -o benchmarks/bench_startup_podman.md scripts/benchmark.star:bench_startup_podman
build/tinyrange build -o benchmarks/bench_startup_tinyrange.md scripts/benchmark.star:bench_startup_tinyrange
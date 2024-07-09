#!/bin/bash

set -ex

./build.go

build/pkg2 -o benchmarks/bench_startup_docker.md -rebuild -build bench_startup_docker scripts/benchmark.star
build/pkg2 -o benchmarks/bench_startup_podman.md -rebuild -build bench_startup_podman scripts/benchmark.star
build/pkg2 -o benchmarks/bench_startup_tinyrange.md -rebuild -build bench_startup_tinyrange scripts/benchmark.star
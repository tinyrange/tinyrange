#!/bin/bash

set -ex

./tools/build.go

HOSTNAME=$(hostname)

mkdir -p benchmarks/$HOSTNAME

BENCHMARKS="$BENCHMARKS bench_startup_docker bench_startup_podman bench_startup_tinyrange"
BENCHMARKS="$BENCHMARKS bench_cpu_docker bench_memory_docker bench_fileio_docker"
BENCHMARKS="$BENCHMARKS bench_cpu_podman bench_memory_podman bench_fileio_podman"
BENCHMARKS="$BENCHMARKS bench_cpu_tinyrange bench_memory_tinyrange bench_fileio_tinyrange"

for i in $BENCHMARKS
do 
    build/tinyrange build -o benchmarks/$HOSTNAME/$i.txt scripts/benchmark.star:$i
done
#!/usr/bin/env python3

import matplotlib.pyplot as plt
from absl import app, logging, flags
from json import loads
import pandas as pd


def main(args):
    start_time = []
    times = []
    commands = []
    with open("benchmarks/results.njson") as f:
        for line in f:
            t, _, obj = line.partition(":")
            obj = loads(obj)
            run_times = obj["results"][0]["times"]
            start_time += [float(t)] * len(run_times)
            times += run_times
            commands += [obj["results"][0]["command"]] * len(run_times)

    df = pd.DataFrame(dict(start_time=start_time, times=times, commands=commands))

    groups = df.groupby("commands")

    fig, ax = plt.subplots()
    ax.margins(0.05)  # Optional, just adds 5% padding to the autoscaling
    for name, group in groups:
        ax.plot(
            group.start_time, group.times, marker="o", linestyle="", ms=12, label=name
        )
    ax.legend()
    ax.set_ylabel("runtime (seconds)")

    plt.show()


if __name__ == "__main__":
    app.run(main)

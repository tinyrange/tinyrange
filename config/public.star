"""
Minimal Config with currently public fetchers.
"""

load(
    "repos/alpine.star",
    "add_alpine_fetchers",
)
load(
    "repos/debian.star",
    "add_ubuntu_fetchers",
)

# Alpine Linux
add_alpine_fetchers(only_latest = False)

# Ubuntu
add_ubuntu_fetchers(only_latest = True)

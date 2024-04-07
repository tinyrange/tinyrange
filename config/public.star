"""
Minimal Config with currently public fetchers.
"""

load(
    "repos/alpine.star",
    "add_alpine_fetchers",
)

# Alpine Linux
add_alpine_fetchers(only_latest = True)

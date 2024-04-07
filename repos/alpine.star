"""
Repository configuration for Alpine.
"""

load("fetchers/alpine.star", "fetch_alpine_repository")

alpine_mirror = "https://dl-cdn.alpinelinux.org/alpine"

latest_alpine_version = "v3.19"

def add_alpine_fetchers(only_latest = True):
    for version in ["v3.19"]:
        for repo in ["main", "community"]:
            for arch in ["x86_64"]:
                if only_latest and version != latest_alpine_version:
                    continue

                fetch_repo(
                    fetch_alpine_repository,
                    ("{}/{}/{}/{}".format(alpine_mirror, version, repo, arch), repo),
                    distro = "alpine@{}".format(version),
                )

if __name__ == "__main__":
    add_alpine_fetchers()

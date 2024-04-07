"""
Repository configuration for Alpine.
"""

load("fetchers/alpine.star", "fetch_alpine_repository")

alpine_mirror = "https://dl-cdn.alpinelinux.org/alpine"

alpine_versions = [
    "edge",
    "v3.0",
    "v3.1",
    "v3.2",
    "v3.3",
    "v3.4",
    "v3.5",
    "v3.6",
    "v3.7",
    "v3.8",
    "v3.9",
    "v3.10",
    "v3.11",
    "v3.12",
    "v3.13",
    "v3.14",
    "v3.15",
    "v3.16",
    "v3.17",
    "v3.18",
    "v3.19",
]

latest_alpine_version = "v3.19"

def add_alpine_fetchers(only_latest = True):
    for version in alpine_versions:
        for repo in ["main", "community", "testing"]:
            for arch in ["x86_64"]:
                if only_latest and version != latest_alpine_version:
                    continue

                fetch_repo(
                    fetch_alpine_repository,
                    ("{}/{}/{}/{}".format(alpine_mirror, version, repo, arch), repo),
                    distro = "alpine@{}".format(version),
                )

def add_wolfi_fetchers():
    for arch in ["x86_64"]:
        fetch_repo(
            fetch_alpine_repository,
            ("https://packages.wolfi.dev/os/{}".format(arch), "os"),
            distro = "wolfi",
        )

def add_postmarketos_fetchers(only_latest = True):
    for version in ["master", "staging", "v20.05", "v21.03", "v21.06", "v21.12", "v22.06", "v22.12", "v23.06", "v23.12"]:
        for arch in ["x86_64"]:
            if only_latest and version != "master":
                continue

            fetch_repo(
                fetch_alpine_repository,
                ("https://mirror.postmarketos.org/postmarketos/{}/{}".format(version, arch), "postmarketos"),
                distro = "postmarketos@{}".format(version),
            )

if __name__ == "__main__":
    add_alpine_fetchers()

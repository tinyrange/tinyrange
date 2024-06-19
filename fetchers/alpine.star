db.add_mirror("alpine", ["https://dl-cdn.alpinelinux.org/alpine"])

LATEST_ALPINE_VERSION = "3.20"

ALPINE_VERSIONS = [
    "edge",
    "3.20",
    "3.19",
    "3.18",
    "3.17",
    "3.16",
    "3.15",
    "3.14",
    "3.13",
    "3.12",
    "3.11",
    "3.10",
    "3.9",
    "3.8",
    "3.7",
    "3.6",
    "3.5",
    "3.4",
    "3.3",
    "3.2",
    "3.1",
    "3.0",
]

def parse_alpine_repo(ctx, index):
    index = index["APKINDEX"]

    # Create a record writer which writes starlark objects to a file.
    ret = ctx.recordwriter()

    pkg = {}

    for line in index.read().split("\n"):
        if len(line) == 0:
            ret.emit(pkg)
        else:
            k, v = line.split(":", 1)
            pkg[k] = v

    if len(pkg) != 0:
        ret.emit(pkg)

    return ret

def split_alpine_name(q):
    n, v = "", ""

    if ">=" in q:
        n, _, v = q.partition(">=")
        v = ">" + v
    elif "=" in q:
        n, _, v = q.partition("=")
    else:
        n = q

    n = n.replace(":", "_")
    return n, v

def parse_alpine_query(q):
    n, v = split_alpine_name(q)

    return query(n + ":" + v)

def parse_alpine_alias(q):
    n, v = split_alpine_name(q)

    return name(name = n, version = v)

def parse_alpine_packages(ctx, ent):
    ctx.add_package(package(
        name = name(
            name = ent["P"].replace(":", "_"),
            version = ent["V"],
        ),
        installers = [
            installer(
                tags = ["level1"],
                # This is a basic package defintion that just uses apk to install the package.
                directives = [
                    directive.run_command("apk add {}".format(ent["P"])),
                ],
            ),
            installer(
                tags = ["level2"],
                directives = [
                    directive.run_command("apk add {}".format(ent["P"])),
                ],
                dependencies = [parse_alpine_query(dep) for dep in ent["D"].split(" ")] if "D" in ent else [],
            ),
        ],
        aliases = [parse_alpine_alias(provides) for provides in ent["p"].split(" ")] if "p" in ent else [],
        raw = ent,
    ))

def make_alpine_repos(only_latest = True):
    alpine_repos = {}

    for version in ALPINE_VERSIONS:
        if only_latest and version != LATEST_ALPINE_VERSION:
            continue

        repos = []

        repo_list = ["main"]

        server_version = version

        if version == "edge":
            repo_list.append("community")
            repo_list.append("testing")
        else:
            server_version = "v" + server_version
            if int(version.split(".")[1]) > 3:
                repo_list.append("community")

        for repo in repo_list:
            repos.append(define.build(
                parse_alpine_repo,
                define.read_archive(
                    define.fetch_http(
                        "mirror://alpine/{}/{}/{}/APKINDEX.tar.gz".format(server_version, repo, "x86_64"),
                        expire_time = duration("8h"),
                    ),
                    ".tar.gz",
                ),
            ))

        # Define a package collection containing all the repos.
        alpine_repos[version] = define.package_collection(
            parse_alpine_packages,
            *repos
        )

    return alpine_repos

def make_alpine_builders(repos):
    ret = []
    for version in repos:
        # Define a container builder for each version.
        ret.append(define.container_builder(
            name = "alpine@" + version,
            display_name = "Alpine " + version,
            base_directives = [
                # All containers made with this builder start with a base image.
                directive.base_image("library/alpine:" + version),
            ],
            # This builder is scoped to just the packages in this repo.
            packages = repos[version],
        ))

    return ret

if __name__ == "__main__":
    for builder in make_alpine_builders(make_alpine_repos(only_latest = False)):
        db.add_container_builder(builder)

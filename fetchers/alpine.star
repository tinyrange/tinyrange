db.add_mirror("alpine", ["https://dl-cdn.alpinelinux.org/alpine"])

def parse_alpine_repo(ctx, url):
    # Read the index in one line by requesting the URL.
    index = ctx.read_archive(
        define.fetch_http(url + "/APKINDEX.tar.gz"),
        ".tar.gz",
    )["APKINDEX"]

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

def parse_alpine_packages(ctx, ent):
    ctx.add_package(package(
        name = name(
            name = ent["P"],
            version = ent["V"],
        ),
        directives = [
            # This is a basic package defintion that just uses apk to install the package.
            directive.run_command("apk add {}".format(ent["P"])),
        ],
    ))

def make_alpine_repos(only_latest = True):
    alpine_repos = {}

    for version in ["3.20"]:
        repos = []
        for repo in ["main", "community"]:
            repos.append(define.build(
                parse_alpine_repo,
                "mirror://alpine/v{}/{}/{}".format(version, repo, "x86_64"),
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
    for builder in make_alpine_builders(make_alpine_repos(only_latest = True)):
        db.add_container_builder(builder)

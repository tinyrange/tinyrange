"""
Conda Package Fetcher
"""

load("common/common.star", "opt", "split_maybe")

def parse_conda_name(ctx, name, arch):
    name, version = split_maybe(name, " ", 2)
    return ctx.name(name = name, version = version, architecture = arch)

def fetch_conda_repository(ctx, url, architecture):
    resp = fetch_http(url + "repodata.json.bz2").read_compressed(".bz2")

    data = json.decode(resp.read())

    for filename in data["packages"]:
        ent = data["packages"][filename]

        # distro = "conda@{}".format(ent["build"])

        pkg = ctx.add_package(ctx.name(
            name = ent["name"],
            version = ent["version"],
            architecture = architecture,
        ))

        pkg.set_license(opt(ent, "license"))

        for depend in ent["depends"]:
            pkg.add_dependency(parse_conda_name(ctx, depend, architecture))

    if "packages.conda" in data:
        for filename in data["packages.conda"]:
            pkg = data["packages.conda"][filename]
            break

"""
Conda Package Fetcher
"""

def opt(d, key, default = ""):
    if key in d:
        if d[key] != None:
            return d[key]
    return default

def split_maybe(s, split, count, default = ""):
    ret = []

    if s != None:
        tokens = s.split(split, count - 1)
        for tk in tokens:
            ret.append(tk)
        for _ in range(count - len(tokens)):
            ret.append(default)
    else:
        for _ in range(count):
            ret.append(default)

    return ret

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

"""
Debian Package Fetcher
"""

ubuntu_versions = {
    "mantic": "23.10",
    "lunar": "23.04",
    "kinetic": "22.10",
    "jammy": "22.04",
    "impish": "21.10",
    "hirsute": "21.04",
    "groovy": "20.10",
    "focal": "20.04",
    "eoan": "19.10",
    "disco": "19.04",
    "cosmic": "18.10",
    "bionic": "18.04",
    "artful": "17.10",
    "zesty": "17.04",
    "yakkety": "16.10",
    "xenial": "16.04",
    "wily": "15.10",
    "vivid": "15.04",
    "utopic": "14.10",
    "trusty": "14.04",
    "saucy": "13.10",
    "raring": "13.04",
    "quantal": "12.10",
    "precise": "12.04",
    "oneiric": "11.10",
    "natty": "11.04",
    "maverick": "10.10",
    "lucid": "10.04",
    "karmic": "9.10",
    "jaunty": "9.04",
    "intrepid": "8.10",
    "hardy": "8.04",
    "gutsy": "7.10",
    "feisty": "7.04",
    "edgy": "6.10",
    "dapper": "6.06",
    "breezy": "5.10",
    "hoary": "5.04",
    "warty": "4.10",
}

def parse_debian_index(contents):
    lines = contents.splitlines()

    ret = []
    ent = {}
    last_ent = None

    for line in lines:
        if ": " in line:
            key, value = line.split(": ", 1)
            ent[key.lower()] = value
            last_ent = key
        elif len(line) == 0:
            ret.append(ent)
            ent = {}
        else:
            print(line)
            error("not implemented")

    return ret

def opt(d, key, default = ""):
    if key in d:
        return d[key]
    else:
        return default

def split_dict_maybe(d, key, split):
    if key in d:
        return d[key].split(split)
    else:
        return []

def parse_debian_name(ctx, name, arch):
    options = name.split(" | ")
    if len(options) > 1:
        return [parse_debian_name(ctx, option, arch) for option in options]
    else:
        version = ""
        arch = ""
        if " (" in name:
            name, version = name.split(" (", 1)
            version = version.removesuffix(")")

        if ":" in name:
            name, arch = name.split(":", 1)

        return ctx.name(name = name, version = version, architecture = arch)

def fetch_debian_repository(ctx, base, url):
    packages_url = "{}/{}/Packages.gz".format(base, url)
    packages_contents = fetch_http(packages_url).read_compressed(".gz")

    contents = parse_debian_index(packages_contents.read())

    for ent in contents:
        pkg = ctx.add_package(ctx.name(
            name = ent["package"],
            version = ent["version"],
            architecture = ent["architecture"],
        ))

        pkg.set_description(opt(ent, "description"))
        pkg.set_installed_size(int(opt(ent, "installed-size", default = -1)))
        pkg.set_size(int(ent["size"]))

        pkg.add_source(url = base + "/" + ent["filename"])

        # pkg.add_metadata("raw", json.encode(ent))

        for depend in split_dict_maybe(ent, "pre-depends", ", "):
            pkg.add_dependency(parse_debian_name(ctx, depend, pkg.arch), kind = "pre")

        for depend in split_dict_maybe(ent, "depends", ", "):
            pkg.add_dependency(parse_debian_name(ctx, depend, pkg.arch))

        for depend in split_dict_maybe(ent, "recommends", ", "):
            pkg.add_dependency(parse_debian_name(ctx, depend, pkg.arch), kind = "soft")

        for alias in split_dict_maybe(ent, "provides", ", "):
            pkg.add_alias(parse_debian_name(ctx, alias, pkg.arch))

        for replaces in split_dict_maybe(ent, "provides", ", "):
            pkg.add_alias(parse_debian_name(ctx, replaces, pkg.arch), kind = "replace")

        for conflict in split_dict_maybe(ent, "conflicts", ", "):
            pkg.add_alias(parse_debian_name(ctx, conflict, pkg.arch), kind = "conflict")

for version in ["jammy"]:
    for pool in ["main", "universe", "multiverse", "restricted"]:
        for arch in ["amd64"]:
            fetch_repo(
                fetch_debian_repository,
                (
                    "http://au.archive.ubuntu.com/ubuntu",
                    "dists/{}/{}/binary-{}".format(version, pool, arch),
                ),
                distro = "ubuntu@{}".format(version),
            )

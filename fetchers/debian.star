"""
Debian Package Fetcher
"""

load("common/common.star", "opt", "split_dict_maybe")

def parse_debian_index(contents):
    lines = contents.splitlines()

    ret = []
    ent = {}
    last_ent = None

    for line in lines:
        if ": " in line:
            key, value = line.split(": ", 1)
            ent[key.lower()] = value
            last_ent = key.lower()
        elif ":" in line:
            key = line.removesuffix(":")
            ent[key.lower()] = ""
            last_ent = key.lower()
        elif line.startswith(" ") or line.startswith("\t"):
            ent[last_ent] += line.strip() + "\n"
        elif len(line) == 0:
            ret.append(ent)
            ent = {}
        else:
            error("line not implemented: " + line)

    return ret

debian_architectures = {
    "amd64": "x86_64",
    "x32": "x32",
    "all": "any",
    "arm64": "aarch64",
    "powerpc": "powerpc",
    "ppc64el": "ppc64le",
    "armel": "armv7",
    "mips": "mips",
    "mipsn32": "mipsn32",
    "mipsn32el": "mipsn32el",
    "mipsn32r6": "mipsn32r6",
    "mipsn32r6el": "mipsn32r6el",
    "mipsr6": "mipsr6",
    "mipsr6el": "mipsr6el",
    "mipsel": "mipsel",
    "mips64": "mips64",
    "mips64el": "mips64el",
    "mips64r6": "mips64r6",
    "mips64r6el": "mips64r6el",
}

def get_debian_architecture(arch):
    if arch.endswith("-cross"):
        arch = arch.split("-")[0]

    if arch in debian_architectures:
        return debian_architectures[arch]
    else:
        return arch

def parse_debian_version(ver):
    if ver.startswith("="):
        return ver.removeprefix("=").strip()
    elif ver.startswith(">="):
        return ">" + ver.removeprefix(">=").strip()
    elif ver.startswith("<="):
        return "<" + ver.removeprefix("<=").strip()
    elif ver.startswith(">>"):
        return ">" + ver.removeprefix(">>").strip()
    elif ver.startswith("<<"):
        return "<" + ver.removeprefix("<<").strip()
    elif ver.startswith(">"):
        return ">" + ver.removeprefix(">").strip()
    elif ver.startswith("<"):
        return "<" + ver.removeprefix("<").strip()
    else:
        return error("unimplemented version: " + ver)

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
            version = parse_debian_version(version)

        if ":" in name:
            name, arch = name.split(":", 1)

        return ctx.name(name = name, version = version, architecture = get_debian_architecture(arch))

def fetch_debian_repository(ctx, base, fallback, url):
    repo = base
    packages_url = "{}/{}/Packages.gz".format(base, url)
    packages_resp = fetch_http(packages_url)

    if packages_resp == None:
        if fallback == None:
            return None  # Nothing we can do.

        packages_url = "{}/{}/Packages.gz".format(fallback, url)
        packages_resp = fetch_http(packages_url)
        if packages_resp == None:
            return None  # Assume the package doesn't exist.

        repo = fallback

    packages_contents = packages_resp.read_compressed(".gz")

    contents = parse_debian_index(packages_contents.read())

    for ent in contents:
        pkg = ctx.add_package(ctx.name(
            name = ent["package"],
            version = ent["version"],
            architecture = get_debian_architecture(ent["architecture"]),
        ))

        pkg.set_raw(json.encode(ent))

        pkg.set_description(opt(ent, "description"))
        pkg.set_installed_size(int(opt(ent, "installed-size", default = -1)))
        pkg.set_size(int(ent["size"]))

        pkg.add_source(kind = "deb", url = repo + "/" + ent["filename"])

        # pkg.add_metadata("raw", json.encode(ent))

        for depend in split_dict_maybe(ent, "pre-depends", ", "):
            pkg.add_dependency(parse_debian_name(ctx, depend, pkg.arch), kind = "pre")

        for depend in split_dict_maybe(ent, "depends", ", "):
            pkg.add_dependency(parse_debian_name(ctx, depend, pkg.arch))

        for depend in split_dict_maybe(ent, "recommends", ", "):
            pkg.add_dependency(parse_debian_name(ctx, depend, pkg.arch), recommends = True)

        for alias in split_dict_maybe(ent, "provides", ", "):
            pkg.add_alias(parse_debian_name(ctx, alias, pkg.arch))

        # for replaces in split_dict_maybe(ent, "replaces", ", "):
        #     pkg.add_alias(parse_debian_name(ctx, replaces, pkg.arch), kind = "replace")

        for conflict in split_dict_maybe(ent, "conflicts", ", "):
            pkg.add_conflict(parse_debian_name(ctx, conflict, pkg.arch))

    return None

def parse_deb(f):
    archive = f.read_archive(".ar")

    control = None
    data = None

    for f in archive:
        if f.name.startswith("control."):
            control = f.read_archive(f.name)
        elif f.name.startswith("data."):
            data = f.read_archive(f.name)

    return control, data

def get_deb_contents(ctx, url):
    resp = fetch_http(url, fast = True)

    _, data = parse_deb(resp)

    return data

register_content_fetcher(
    "deb",
    get_deb_contents,
    (),
)

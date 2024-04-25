"""
Package Fetcher for OpenWRT
"""

load("common/common.star", "split_dict_maybe")

def parse_openwrt_packages(contents):
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

def parse_openwrt_name(ctx, name, arch):
    options = name.split(" | ")
    if len(options) > 1:
        return [parse_openwrt_name(ctx, option, arch) for option in options]
    else:
        version = ""
        arch = ""
        if " (" in name:
            name, version = name.split(" (", 1)
            version = version.removesuffix(")")

        if ":" in name:
            name, arch = name.split(":", 1)

        return ctx.name(name = name, version = version, architecture = arch)

openwrt_architectures = {
    "x86_64": "x86_64",
    "all": "any",
}

def get_openwrt_architecture(arch):
    if arch in openwrt_architectures:
        return openwrt_architectures[arch]
    else:
        return arch

def fetch_openwrt_repostiory(ctx, url):
    packages_url = url + "Packages.gz"
    packages_contents = fetch_http(packages_url).read_compressed(packages_url)

    packages = parse_openwrt_packages(packages_contents.read())

    for info in packages:
        pkg = ctx.add_package(ctx.name(
            name = info["package"],
            version = info["version"],
            architecture = get_openwrt_architecture(info["architecture"]),
        ))

        pkg.set_raw(json.encode(info))

        if "description" in info:
            pkg.set_description(info["description"])
        pkg.add_source(kind = "openwrt", url = url + info["filename"])

        for depend in split_dict_maybe(info, "depends", ", "):
            pkg.add_dependency(parse_openwrt_name(ctx, depend, info["architecture"]))

        for provide in split_dict_maybe(info, "provides", ", "):
            pkg.add_alias(parse_openwrt_name(ctx, provide, info["architecture"]))

if __name__ == "__main__":
    fetch_repo(
        fetch_openwrt_repostiory,
        ("https://mirror-03.infra.openwrt.org/releases/packages-23.05/x86_64/base/",),
        distro = "openwrt@23.05",
    )

"""
XBPS Package Fetcher
"""

def parse_xbps_name(ctx, name):
    if ">=" in name:
        name, version = name.split(">=", 1)
        return ctx.name(
            name = name,
            version = ">" + version,
        )
    elif "<" in name:
        name, version = name.split("<", 1)
        return ctx.name(
            name = name,
            version = "<" + version,
        )
    elif "-" in name:
        name, version = name.rsplit("-", 1)
        return ctx.name(
            name = name,
            version = version,
        )
    else:
        return error("could not parse: " + name)

xbps_architectures = {
    "x86_64-musl": "x86_64",
    "x86_64": "x86_64",
    "noarch": "any",
}

def get_xbps_architecture(arch):
    if arch in xbps_architectures:
        return xbps_architectures[arch]
    else:
        return arch

def fetch_xbps_repository(ctx, url, arch):
    repodata_url = "{}/{}-repodata".format(url, arch)

    repodata_archive = fetch_http(repodata_url).read_archive(".tar.zst")

    repodata = parse_plist(repodata_archive["index.plist"].read())

    for name in repodata:
        ent = repodata[name]

        version = ent["pkgver"].removeprefix(name + "-")
        pkg = ctx.add_package(ctx.name(
            name = name,
            version = version,
            architecture = get_xbps_architecture(ent["architecture"]),
        ))
        pkg.set_license(ent["license"])
        pkg.set_description(ent["short_desc"])
        if "filename_size" in ent:
            pkg.set_size(ent["filename_size"])
        pkg.set_installed_size(ent["installed_size"])

        download_url = "{}/{}.{}.xbps".format(url, ent["pkgver"], arch)
        pkg.add_source(url = download_url)

        if "run_depends" in ent:
            for depend in ent["run_depends"]:
                pkg.add_dependency(parse_xbps_name(ctx, depend))

        if "provides" in ent:
            for depend in ent["provides"]:
                pkg.add_alias(parse_xbps_name(ctx, depend))

"""
Package Fetcher for RPM Repositories.
"""

def opt(d, key, default = ""):
    if key in d:
        if d[key] != None:
            return d[key]
    return default

def fetch_rpm_repostiory(ctx, url):
    repomd_url = url + "repodata/repomd.xml"

    repomd = parse_xml(fetch_http(repomd_url).read())

    primary = [
        element
        for element in repomd["repomd"]["data"]
        if element["-type"] == "primary"
    ][0]

    primary_url = primary["location"]["-href"]

    primary = parse_xml(fetch_http(url + primary_url).read_compressed(primary_url).read())

    for ent in primary["metadata"]["package"]:
        pkg = ctx.add_package(ctx.name(
            name = ent["name"],
            version = ent["version"]["-ver"],
            architecture = ent["arch"],
        ))

        pkg.set_description(ent["description"])
        pkg.set_license(opt(ent, "license"))
        pkg.add_source(url = url + ent["location"]["-href"])

        require_list = opt(ent["format"], "requires", default = {"entry": []})["entry"]
        if type(require_list) == "dict":
            require_list = [require_list]
        for require in require_list:
            version = opt(require, "-ver")
            pkg.add_dependency(ctx.name(
                name = require["-name"],
                version = version,
            ))

        provides_list = ent["format"]["provides"]["entry"]
        if type(provides_list) == "dict":
            provides_list = [provides_list]
        for provide in provides_list:
            version = opt(provide, "-ver")
            pkg.add_alias(ctx.name(
                name = provide["-name"],
                version = version,
            ))

        file_list = opt(ent["format"], "file", default = [])
        if type(file_list) != "list":
            file_list = [file_list]
        for ent in file_list:
            if type(ent) == "dict":
                ent = ent["#content"]
            pkg.add_alias(ctx.name(
                name = ent,
            ))

fetch_repo(
    fetch_rpm_repostiory,
    ("https://mirror.aarnet.edu.au/pub/fedora/linux/updates/39/Everything/x86_64/",),
    distro = "fedora@39",
)

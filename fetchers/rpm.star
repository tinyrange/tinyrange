"""
Package Fetcher for RPM Repositories.
"""

load("common/common.star", "opt")

def fetch_rpm_repostiory(ctx, url):
    repomd_url = url + "repodata/repomd.xml"

    repomd = parse_xml(fetch_http(repomd_url).read())

    primary = [
        element
        for element in repomd["repomd"]["data"]
        if element["-type"] == "primary"
    ][0]

    primary_url = primary["location"]["-href"]
    primary_size = 0
    if "size" in primary:
        primary_size = int(primary["size"])

    primary = parse_xml(fetch_http(
        url + primary_url,
        expected_size = primary_size,
    ).read_compressed(primary_url).read())

    if "package" not in primary["metadata"]:
        # No Packages
        return

    package_list = primary["metadata"]["package"]
    if type(package_list) == "dict":
        package_list = [package_list]
    for ent in package_list:
        pkg = ctx.add_package(ctx.name(
            name = ent["name"],
            version = ent["version"]["-ver"],
            architecture = ent["arch"],
        ))

        if "description" in ent:
            pkg.set_description(ent["description"])
        pkg.set_license(opt(ent, "license"))
        pkg.add_source(url = url + ent["location"]["-href"])

        require_list = opt(ent["format"], "requires", default = {"entry": []})
        if "entry" in require_list:
            require_list = require_list["entry"]
            if type(require_list) == "dict":
                require_list = [require_list]
            for require in require_list:
                version = opt(require, "-ver")
                pkg.add_dependency(ctx.name(
                    name = require["-name"],
                    version = version,
                ))

        if "provides" in ent["format"]:
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

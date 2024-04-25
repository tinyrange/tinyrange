"""
Package Fetcher for RPM Repositories.
"""

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

    primary = fetch_http(
        url + primary_url,
        expected_size = primary_size,
    ).read_compressed(primary_url).read_rpm_xml()

    for ent in primary:
        arch = ent["Arch"]
        if arch == "noarch":
            arch = "any"

        pkg = ctx.add_package(ctx.name(
            name = ent["Name"],
            version = ent["Version"]["Ver"],
            architecture = arch,
        ))

        if "Description" in ent:
            pkg.set_description(ent["Description"])
        pkg.set_license(ent["Format"]["License"])
        pkg.add_source(kind = "rpm", url = url + ent["Location"]["Href"])

        if ent["Format"]["Requires"]["Entry"] != None:
            for require in ent["Format"]["Requires"]["Entry"]:
                pkg.add_dependency(ctx.name(
                    name = require["Name"],
                    version = require["Ver"],
                ))

        if ent["Format"]["Provides"]["Entry"] != None:
            for provide in ent["Format"]["Provides"]["Entry"]:
                pkg.add_alias(ctx.name(
                    name = provide["Name"],
                    version = provide["Ver"],
                ))

        if ent["Format"]["File"] != None:
            for ent in ent["Format"]["File"]:
                pkg.add_alias(ctx.name(
                    name = ent["Text"],
                ))

"""
Support for fetching nodejs packages with pkg2.
"""

registry_url = "https://registry.npmjs.org/"

def parse_node_version(ver):
    if ver.startswith("~"):
        return ver.removeprefix("~")

    if ver.startswith("^"):
        return ver.removeprefix("^")

    if ver.startswith(">="):
        ver = ver.removeprefix(">=").strip()

    if "<" in ver:
        ver = ver.partition("<")[0].strip()

    if " " not in ver:
        return ver

    return error("version not implemented: " + ver)

def search_provider_node(ctx, name):
    info_url = registry_url + name.name
    info = json.decode(fetch_http(info_url).read())

    if name.version == "":
        name = name.set_version(info["dist-tags"]["latest"])

    if name.version not in info["versions"]:
        return error("package version " + name.version + " not found")
    version_info = info["versions"][name.version]

    pkg = ctx.add_package(name)
    pkg.set_description(version_info["description"])

    if "dependencies" in version_info:
        depend_list = version_info["dependencies"]
        for depend in depend_list:
            pkg.add_dependency(ctx.name(
                name = depend,
                version = parse_node_version(depend_list[depend]),
            ))

    return name

register_search_provider("node", search_provider_node, ())

"""
Support for fetching golang packages with pkg2.
"""

proxy_url = "https://proxy.golang.org/"

def parse_go_version(v):
    if v.startswith("v"):
        major, minor, revision = v.removeprefix("v").split(".")
        major = int(major)
        minor = int(minor)
        revision = int(revision)
        return major, minor, revision
    else:
        return error("version not implemented: " + v)

def compare_go_versions(a, b):
    if a == "":
        return b

    a_major, a_minor, a_revsion = parse_go_version(a)
    b_major, b_minor, b_revsion = parse_go_version(b)

    if a_major > b_major:
        return a
    elif b_major > a_major:
        return b

    if a_minor > b_minor:
        return a
    elif b_minor > a_minor:
        return b

    if a_revsion > b_revsion:
        return a
    elif b_revsion > a_revsion:
        return b

    return a

def get_latest_version(version_list):
    latest = ""
    for version in version_list:
        latest = compare_go_versions(latest, version)
    return latest

def parse_mod_file(contents):
    ret = {
        "name": "",
        "go_version": "",
        "requires": [],
    }

    lines = contents.splitlines()

    in_require_block = False

    for line in lines:
        line = line.strip()
        if len(line) == 0:
            continue
        elif line.startswith("//"):
            continue

        if in_require_block:
            if line == ")":
                in_require_block = False
                continue

            indirect = False
            if "// indirect" in line:
                line = line.partition("//")[0]
                indirect = True
            elif "//" in line:
                line = line.partition("//")[0]

            line = line.strip()

            name, version = line.split(" ")
            ret["requires"].append((name, version, indirect))
        elif line.startswith("module "):
            ret["name"] = line.removeprefix("module ")
        elif line.startswith("go "):
            ret["go_version"] = line.removeprefix("go ")
        elif line == "require (":
            in_require_block = True
        elif line.startswith("require "):
            line = line.removeprefix("require ")

            indirect = False
            if "// indirect" in line:
                line = line.partition("//")[0]
                indirect = True
            elif "//" in line:
                line = line.partition("//")[0]

            line = line.strip()

            name, version = line.split(" ")
            ret["requires"].append((name, version, indirect))
        else:
            return error("line not implemented: " + line)

    return ret

def search_provider_go(ctx, name):
    if name.version == "":
        version_list_url = proxy_url + name.name + "/@v/list"

        version_list = fetch_http(version_list_url).read().splitlines()

        name = name.set_version(get_latest_version(version_list))

    mod_file_url = proxy_url + name.name.lower() + "/@v/" + name.version + ".mod"

    mod_file = parse_mod_file(fetch_http(mod_file_url).read())

    pkg = ctx.add_package(name)

    for name, version, indirect in mod_file["requires"]:
        if indirect:
            continue

        pkg.add_dependency(ctx.name(
            name = name.strip("\""),
            version = version,
        ))

    return name

register_search_provider("go", search_provider_go, ())

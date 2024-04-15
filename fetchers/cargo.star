"""
Support for fetching cargo packages with pkg2.
"""

load("common/fetch.star", "fetch_github_archive")

def parse_cargo_version(v):
    major, minor, revision = v.split(".", 2)
    major = int(major)
    minor = int(minor)
    if "-" in revision:
        revision = revision.split("-")[0]
    if "+" in revision:
        revision = revision.split("+")[0]
    revision = int(revision)
    return major, minor, revision

def compare_cargo_versions(a, b):
    if "vers" not in a:
        return b

    a_major, a_minor, a_revsion = parse_cargo_version(a["vers"])
    b_major, b_minor, b_revsion = parse_cargo_version(b["vers"])

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
    latest = {}
    for version in version_list:
        latest = compare_cargo_versions(latest, version)
    return latest

def match_version(req, ver):
    if req == ">= 0" or req == "*":
        return True

    if req.startswith("="):
        return ver.startswith(req.removeprefix("=").strip())
    elif req.startswith("^"):
        req = req.removeprefix("^")

        ver = ver.rpartition(".")[0]
        if "." in req:
            req = req.rpartition(".")[0]

        return (ver + ".").startswith(req + ".")
    elif req.startswith("~"):
        req = req.removeprefix("~")

        return (ver + ".").startswith(req + ".")
    elif req.endswith(".*.*"):
        req = req.removesuffix(".*.*")

        ver = ver.split(".")[0]

        return ver == req
    elif req.endswith(".*"):
        req = req.removesuffix(".*")

        ver = ver.rpartition(".")[0]

        return ver == req
    else:
        return ver == req

def parse_index_file(ctx, name, file):
    contents = file.read()

    lines = [json.decode(line) for line in contents.splitlines()]

    if len(lines) == 0:
        return None

    if name.version == "":
        name = name.set_version(lines[len(lines) - 1]["vers"])

    version_query = [line for line in lines if match_version(name.version, line["vers"])]

    if len(version_query) == 0:
        return error("version " + name.version + " not found")

    info = get_latest_version(version_query)

    # name = name.set_version(info["vers"])

    pkg = ctx.add_package(name)
    for dep in info["deps"]:
        if dep["target"] != None:
            continue
        if dep["optional"]:
            continue
        if dep["kind"] != "normal":
            continue
        if "package" in dep:
            pkg.add_dependency(ctx.name(
                name = dep["package"],
                version = dep["req"],
            ))
        else:
            pkg.add_dependency(ctx.name(
                name = dep["name"],
                version = dep["req"],
            ))

    return name

def fetch_cargo_repository(ctx):
    repo = fetch_github_archive("rust-lang", "crates.io-index", ref = "master")

    for file in repo:
        if file.name.startswith(".github") or file.name == "config.json":
            continue
        if type(file) != "File":
            continue
        filename = file.name.rpartition("/")[2]
        parse_index_file(ctx, ctx.name(name = filename), file)

if __name__ == "__main__":
    fetch_repo(fetch_cargo_repository, (), distro = "cargo")

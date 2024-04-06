"""
Support for fetching cargo packages with pkg2.
"""

repo = fetch_git("https://github.com/rust-lang/crates.io-index")

repo_master = repo.branch("master")

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

def search_provider_cargo(ctx, name):
    if name.name == "core" or name.name == "alloc" or name.name == "std":
        ctx.add_package(name)
        return name

    if len(name.name) == 1:
        return parse_index_file(ctx, name, repo_master["1/" + name.name])
    elif len(name.name) == 2:
        return parse_index_file(ctx, name, repo_master["2/" + name.name])
    elif len(name.name) == 3:
        return parse_index_file(ctx, name, repo_master["3/" + name.name[0] + "/" + name.name])
    else:
        return parse_index_file(ctx, name, repo_master[name.name[:2] + "/" + name.name[2:4] + "/" + name.name])

register_search_provider("rust", search_provider_cargo, ())

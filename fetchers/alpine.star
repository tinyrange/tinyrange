"""
Alpine Linux Package Fetcher
"""

load("common/common.star", "opt", "split_dict_maybe", "split_maybe")

def parse_apk_index(contents):
    lines = contents.splitlines()

    ret = []
    ent = {}

    for line in lines:
        if len(line) == 0:
            ret.append(ent)
            ent = {}
        else:
            k, v = line.split(":", 1)
            ent[k] = v

    return ret

def parse_apk_version(v):
    if "-r" in v:
        v = v.split("-r")[0]
    return v

def parse_apk_name(ctx, s):
    pkg = ""
    version = ""
    if ">=" in s:
        pkg, version = s.split(">=", 1)
        version = ">" + version
    elif ">" in s:
        pkg, version = s.split(">", 1)
        version = ">" + version
    elif "<=" in s:
        pkg, version = s.split("<=", 1)
        version = "<" + version
    elif "<" in s:
        pkg, version = s.split("<", 1)
        version = "<" + version
    elif "~" in s:
        pkg, version = s.split("~", 1)
        version = "~" + version
    else:
        pkg, version = split_maybe(s, "=", 2)
    return ctx.name(name = pkg, version = parse_apk_version(version))

def fetch_alpine_repository(ctx, url, repo):
    ctx.pledge(semver = True)

    resp = fetch_http(url + "/APKINDEX.tar.gz")

    if resp == None:
        return

    apk_index = resp.read_archive(".tar.gz")["APKINDEX"]

    contents = parse_apk_index(apk_index.read())

    for ent in contents:
        pkg = ctx.add_package(ctx.name(
            name = ent["P"],
            version = parse_apk_version(ent["V"]),
            architecture = ent["A"],
        ))

        pkg.set_raw(json.encode(ent))

        pkg.set_description(ent["T"])
        if "L" in ent:
            pkg.set_license(ent["L"])
        pkg.set_size(int(ent["S"]))
        if "I" in ent:
            pkg.set_installed_size(int(ent["I"]))

        pkg.add_source(kind = "apk", url = "{}/{}-{}.apk".format(url, pkg.name, ent["V"]))
        if opt(ent, "c") != "":
            pkg.add_build_script("alpine", (ent["c"], "{}/{}/APKBUILD".format(repo, ent["o"])))

        pkg.add_metadata("url", opt(ent, "U"))
        pkg.add_metadata("origin", opt(ent, "o"))
        pkg.add_metadata("commit", opt(ent, "c"))
        pkg.add_metadata("maintainer", opt(ent, "m"))

        for depend in split_dict_maybe(ent, "D", " "):
            if depend.startswith("!"):
                pkg.add_conflict(parse_apk_name(ctx, depend.removeprefix("!")))
            else:
                pkg.add_dependency(parse_apk_name(ctx, depend))

        for alias in split_dict_maybe(ent, "p", " "):
            pkg.add_alias(parse_apk_name(ctx, alias))

def fetch_alpine_build_script(ctx, url, commit, file):
    repo = fetch_git(url)
    tree = repo[commit]
    build_script = tree[file].read()

    script_vars = shell_context().eval(build_script)

    sources = script_vars["source"].split("\n")

    return [source.strip("\t\n") for source in sources if source.strip("\t\n") != ""]

register_script_fetcher(
    "alpine",
    fetch_alpine_build_script,
    ("git://git.alpinelinux.org/aports",),
)

def parse_pkg_info(contents):
    ret = {}

    for line in contents.splitlines():
        if line.startswith("#"):
            continue
        k, v = line.split(" = ", 1)
        ret[k] = v

    return ret

def get_apk_contents(ctx, pkg, url):
    fs = filesystem()

    ark = fetch_http(url).read_archive(".tar.gz")

    pkg_info = None

    for file in ark:
        if file.name.startswith("."):
            if file.name == ".PKGINFO":
                pkg_info = parse_pkg_info(file.read())
                continue
            elif file.name.startswith(".SIGN.RSA"):
                # TODO(joshua): Validate signatures.
                continue
            elif file.name == ".pre-install":
                fs[".pkg/apk/pre-install/" + pkg.name + ".sh"] = file
                continue
            elif file.name == ".post-install":
                fs[".pkg/apk/post-install/" + pkg.name + ".sh"] = file
                continue
            elif file.name == ".pre-upgrade":
                # Not needed since we are always installing not upgrading.
                continue
            elif file.name == ".post-upgrade":
                # Not needed since we are always installing not upgrading.
                continue
            elif file.name == ".trigger":
                fs[".pkg/apk/trigger/" + pkg.name + ".txt"] = json.encode(pkg_info["triggers"].split(" "))
                fs[".pkg/apk/trigger/" + pkg.name + ".sh"] = file
                continue
            elif file.name == ".dummy":
                continue
            else:
                return error("hidden file not implemented: " + file.name)
        if file.name.endswith("/"):
            continue  # ignore directoriess
        fs[file.name] = file

    return ctx.archive(fs)

register_content_fetcher(
    "apk",
    get_apk_contents,
    (),
)

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
    return ctx.name(name = pkg, version = version)

def fetch_alpine_repository(ctx, url, repo):
    resp = fetch_http(url + "/APKINDEX.tar.gz")

    if resp == None:
        return

    apk_index = resp.read_archive(".tar.gz")["APKINDEX"]

    contents = parse_apk_index(apk_index.read())

    for ent in contents:
        pkg = ctx.add_package(ctx.name(
            name = ent["P"],
            version = ent["V"],
            architecture = ent["A"],
        ))

        pkg.set_description(ent["T"])
        if "L" in ent:
            pkg.set_license(ent["L"])
        pkg.set_size(int(ent["S"]))
        if "I" in ent:
            pkg.set_installed_size(int(ent["I"]))

        pkg.add_source(url = "{}/{}-{}.apk".format(url, pkg.name, pkg.version))
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

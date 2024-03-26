"""
Arch Linux Package Fetcher
"""

arch_mirror = "https://mirror.aarnet.edu.au/pub/archlinux"

def opt(d, key, default = ""):
    if key in d:
        return d[key]
    else:
        return default

def split_maybe(s, split, count, default = ""):
    ret = []

    if s != None:
        tokens = s.split(split, count - 1)
        for tk in tokens:
            ret.append(tk)
        for _ in range(count - len(tokens)):
            ret.append(default)
    else:
        for _ in range(count):
            ret.append(default)

    return ret

def split_dict_maybe(d, key, split):
    if key in d:
        return d[key].split(split)
    else:
        return []

def parse_arch_package(contents):
    lines = contents.splitlines()

    ret = {}
    current_key = ""
    current_value = ""

    for line in lines:
        if line.startswith("%") and line.endswith("%"):
            if current_value != "":
                ret[current_key.lower()] = current_value.strip("\n")
            current_key = line.strip("%")
            current_value = ""
        else:
            current_value += line + "\n"

    if current_value != "":
        ret[current_key.lower()] = current_value.strip("\n")

    return ret

def parse_arch_name(ctx, name):
    version = ""
    if ">=" in name:
        name, version = name.split(">=", 1)
        version = ">" + version
    else:
        name, version = split_maybe(name, "=", 2)
    return ctx.name(name = name, version = version)

def fetch_arch_repository(ctx, url, pool):
    resp = fetch_http("{}/{}.db.tar.gz".format(url, pool))
    archive = resp.read_archive(".tar.gz")

    for ent in archive:
        if not ent.name.endswith("desc"):
            continue

        ent = parse_arch_package(ent.read())

        pkg = ctx.add_package(ctx.name(
            name = ent["name"],
            version = ent["version"],
            architecture = ent["arch"],
        ))

        pkg.set_description(opt(ent, "desc"))
        pkg.set_license(opt(ent, "license"))
        pkg.set_size(int(ent["csize"]))
        pkg.set_installed_size(int(ent["isize"]))

        pkg.add_source(url = url + "/" + ent["filename"])

        for depend in split_dict_maybe(ent, "depends", "\n"):
            pkg.add_dependency(parse_arch_name(ctx, depend))

        for alias in split_dict_maybe(ent, "provides", "\n"):
            pkg.add_alias(parse_arch_name(ctx, alias))

def fetch_aur_repository(ctx):
    contents = fetch_http("https://aur.archlinux.org/packages-meta-ext-v1.json.gz").read()

    entries = json.decode(contents)

    for ent in entries:
        pkg = ctx.add_package(ctx.name(
            name = ent["Name"],
            version = ent["Version"],
        ))

        if opt(ent, "Description") != None:
            pkg.set_description(opt(ent, "Description"))
        pkg.set_license(",".join(opt(ent, "License", default = [])))

        pkg.add_source(url = "https://aur.archlinux.org" + ent["URLPath"])

        for depend in opt(ent, "Depends", default = []):
            pkg.add_dependency(parse_arch_name(ctx, depend))

        for alias in opt(ent, "Provides", default = []):
            pkg.add_alias(parse_arch_name(ctx, alias))

for pool in ["core", "community", "extra", "multilib"]:
    for arch in ["x86_64"]:
        fetch_repo(
            fetch_arch_repository,
            (
                "{}/{}/os/{}".format(arch_mirror, pool, arch),
                pool,
            ),
            distro = "arch",
        )

fetch_repo(fetch_arch_repository, ("https://repo.bioarchlinux.org/x86_64", "bioarchlinux"), distro="arch")

fetch_repo(fetch_aur_repository, (), distro = "arch")

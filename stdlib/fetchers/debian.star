db.add_mirror("ubuntu", ["https://mirror.aarnet.edu.au/pub/ubuntu/archive"])

LATEST_UBUNTU_VERSION = "jammy"

def parse_debian_index(base, contents):
    lines = contents.splitlines()

    ret = []
    ent = {"$base": base}
    last_ent = None

    for line in lines:
        if ": " in line:
            key, value = line.split(": ", 1)
            ent[key.lower()] = value
            last_ent = key.lower()
        elif ":" in line:
            key = line.removesuffix(":")
            ent[key.lower()] = ""
            last_ent = key.lower()
        elif line.startswith(" ") or line.startswith("\t"):
            ent[last_ent] += line.strip() + "\n"
        elif len(line) == 0:
            ret.append(ent)
            ent = {"$base": base}
        else:
            error("line not implemented: " + line)

    if len(ent) > 1:
        ret.append(ent)

    return ret

def parse_debian_release(ctx, release_file, mirror):
    contents = parse_debian_index("", release_file.read())

    contents = contents[0]

    base = release_file.name.removesuffix("/Release")

    ret = ctx.recordwriter()

    for component in contents["components"].split(" "):
        contents = ctx.build(
            define.decompress_file(
                define.fetch_http(
                    "{}/{}/binary-amd64/Packages.xz".format(base, component),
                    expire_time = duration("8h"),
                ),
                ".xz",
            ),
        )

        contents = parse_debian_index(mirror, contents.read())

        for ent in contents:
            ret.emit(ent)

    return ret

def split_debian_name(q):
    options = [q]

    if " | " in q:
        options = q.split(" | ")

    # TODO(joshua): Support multiple options in the query.
    q = options[0]

    pkg_name = q
    pkg_version = ""

    if " (" in q:
        pkg_name, _, rest = q.partition(" ")
        rest = rest.removeprefix("(").removesuffix(")")

        if rest.startswith(">="):
            pkg_version = ">" + rest.removeprefix(">= ")
        elif rest.startswith(">>"):
            pkg_version = ">" + rest.removeprefix(">> ")
        elif rest.startswith("<<"):
            pkg_version = "<" + rest.removeprefix("<< ")
        elif rest.startswith("="):
            pkg_version = rest.removeprefix("= ")
        else:
            return error("not implemented: " + rest)
    else:
        pkg_version = ""

    return pkg_name, pkg_version

def parse_debian_query(q):
    n, v = split_debian_name(q)

    return query(n + ":" + v)

def parse_debian_alias(q):
    n, v = split_debian_name(q)

    return name(name = n, version = v)

def convert_debian_package(ctx, name, source):
    ar = db.build(define.read_archive(
        source,
        ".ar",
    ))

    data = None
    control = None

    print([f for f in ar])

    for f in ar:
        if f.name.startswith("data."):
            data = filesystem(db.build(define.read_archive(f, f.name)))
        elif f.name.startswith("control."):
            control = filesystem(db.build(define.read_archive(f, f.name)))

    print([f for f in control])

    control = parse_debian_index("", control["control"].read())

    ret = filesystem()

    ret[".pkg/control/{}".format(name)] = json.encode(control)

    for top in data:
        ret[top.name] = top

    return ctx.archive(ret)

def get_debian_installer(pkg, tags):
    ent = pkg.raw

    if tags.contains("level1"):
        return installer(
            directives = [
                # This is a basic package defintion that just uses apt-get to install the package.
                directive.run_command("apt-get install -y {}".format(ent["package"])),
            ],
        )

    deps = []

    if "pre-depends" in ent:
        deps += [parse_debian_query(q) for q in ent["pre-depends"].split(", ")]

    if "depends" in ent:
        deps += [parse_debian_query(q) for q in ent["depends"].split(", ")]

    if "recommends" in ent:
        deps += [parse_debian_query(q) for q in ent["recommends"].split(", ")]

    if tags.contains("level3"):
        download_archive = define.fetch_http(ent["$base"] + ent["filename"])

        return installer(
            directives = [
                define.build(
                    convert_debian_package,
                    ent["package"],
                    download_archive,
                ),
            ],
            dependencies = deps,
        )

    return None

def parse_debian_package(ctx, collection, packages):
    for ent in packages:
        aliases = [
            parse_debian_alias(q)
            for q in ent["provides"].split(", ")
        ] if "provides" in ent else []

        collection.add_package(
            name = name(
                name = ent["package"],
                version = ent["version"],
            ),
            aliases = aliases,
            raw = ent,
        )

def make_ubuntu_repos(only_latest = True):
    ubuntu_repos = {}

    for version in ["jammy"]:
        if only_latest and version != LATEST_UBUNTU_VERSION:
            continue

        ubuntu_repos[version] = define.package_collection(
            parse_debian_package,
            get_debian_installer,
            define.build(
                parse_debian_release,
                define.fetch_http(
                    url = "mirror://ubuntu/dists/{}/Release".format(version),
                    expire_time = duration("8h"),
                ),
                "mirror://ubuntu/",
            ),
        )

    return ubuntu_repos

def build_debian_directives(builder, plan):
    if plan.tags.contains("level3"):
        return plan.directives
    else:
        return [
            define.fetch_oci_image(image = "library/ubuntu", tag = builder.metadata["version"]),
        ] + plan.directives

def make_ubuntu_builders(repos):
    ret = []
    for version in repos:
        # Define a container builder for each version.
        ret.append(define.container_builder(
            name = "ubuntu@" + version,
            display_name = "Ubuntu " + version,
            plan_callback = build_debian_directives,
            # This builder is scoped to just the packages in this repo.
            packages = repos[version],
            metadata = {
                "version": version,
            },
        ))

    return ret

if __name__ == "__main__":
    for builder in make_ubuntu_builders(make_ubuntu_repos(only_latest = True)):
        db.add_container_builder(builder)

db.add_mirror("ubuntu", ["https://mirror.aarnet.edu.au/pub/ubuntu/archive"])

LATEST_UBUNTU_VERSION = "jammy"

def parse_debian_index(contents):
    lines = contents.splitlines()

    ret = []
    ent = {}
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
            ent = {}
        else:
            error("line not implemented: " + line)

    if len(ent) > 0:
        ret.append(ent)

    return ret

def parse_debian_release(ctx, release_file):
    contents = parse_debian_index(release_file.read())

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

        contents = parse_debian_index(contents.read())

        for ent in contents:
            ret.emit(ent)

    return ret

def parse_debian_package(ctx, ent):
    ctx.add_package(package(
        name = name(
            name = ent["package"],
            version = ent["version"],
        ),
        directives = [
            # This is a basic package defintion that just uses apt-get to install the package.
            directive.run_command("apt-get install -y {}".format(ent["package"])),
        ],
        raw = ent,
    ))

def make_ubuntu_repos(only_latest = True):
    ubuntu_repos = {}

    for version in ["jammy"]:
        if only_latest and version != LATEST_UBUNTU_VERSION:
            continue

        ubuntu_repos[version] = define.package_collection(
            parse_debian_package,
            define.build(
                parse_debian_release,
                define.fetch_http(
                    url = "mirror://ubuntu/dists/{}/Release".format(version),
                    expire_time = duration("8h"),
                ),
            ),
        )

    return ubuntu_repos

def make_ubuntu_builders(repos):
    ret = []
    for version in repos:
        # Define a container builder for each version.
        ret.append(define.container_builder(
            name = "ubuntu@" + version,
            display_name = "Ubuntu " + version,
            base_directives = [
                # All containers made with this builder start with a base image.
                directive.base_image("library/ubuntu:" + version),
            ],
            # This builder is scoped to just the packages in this repo.
            packages = repos[version],
        ))

    return ret

if __name__ == "__main__":
    for builder in make_ubuntu_builders(make_ubuntu_repos(only_latest = True)):
        db.add_container_builder(builder)

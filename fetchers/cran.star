def parse_cran_index(contents):
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

    return ret

def parse_cran_name(ctx, name, arch):
    options = name.split(" | ")
    if len(options) > 1:
        return [parse_cran_name(ctx, option, arch) for option in options]
    else:
        version = ""
        arch = ""
        if " (" in name:
            name, version = name.split(" (", 1)
            version = version.removesuffix(")")

        if ":" in name:
            name, arch = name.split(":", 1)

        return ctx.name(name = name, version = version, architecture = arch)

def fetch_cran_repostiory(ctx, url):
    contents = fetch_http(url + "/PACKAGES.gz").read_compressed(".gz").read()

    ents = parse_cran_index(contents)

    for ent in ents:
        pkg = ctx.add_package(ctx.name(
            name = ent["package"],
            version = ent["version"],
        ))

        if "license" in ent:
            pkg.set_license(ent["license"])

        if "path" in ent:
            pkg.add_source(kind = "cran", url = "{}/{}/{}_{}.tar.gz".format(url, ent["path"], pkg.name, pkg.version))
        else:
            pkg.add_source(kind = "cran", url = "{}/{}_{}.tar.gz".format(url, pkg.name, pkg.version))

        if "depends" in ent:
            for depend in ent["depends"].replace("\n", "").split(","):
                pkg.add_dependency(parse_cran_name(ctx, depend.strip(), ""))

        if "imports" in ent:
            for depend in ent["imports"].replace("\n", "").split(","):
                pkg.add_dependency(parse_cran_name(ctx, depend.strip(), ""))

        if "linkingto" in ent:
            for depend in ent["linkingto"].replace("\n", "").split(","):
                pkg.add_dependency(parse_cran_name(ctx, depend.strip(), ""))

if __name__ == "__main__":
    fetch_repo(
        fetch_cran_repostiory,
        ("https://cran.r-project.org/src/contrib",),
        distro = "cran",
    )

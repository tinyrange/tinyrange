db.add_mirror("archlinux", ["https://mirror.aarnet.edu.au/pub/archlinux"])

def parse_arch_database(ctx, db, base):
    db = filesystem(db)

    ret = ctx.recordwriter()

    for pkg in db:
        lines = pkg["desc"].read().splitlines()

        ent = {"url_base": base}
        current_key = ""
        current_value = ""

        for line in lines:
            if line.startswith("%") and line.endswith("%"):
                if current_value != "":
                    ent[current_key.lower()] = current_value.strip("\n")
                current_key = line.strip("%")
                current_value = ""
            else:
                current_value += line + "\n"

        if current_value != "":
            ent[current_key.lower()] = current_value.strip("\n")

        ret.emit(ent)

    return ret

def split_arch_name(q):
    n, v = "", ""

    if ">=" in q:
        n, _, v = q.partition(">=")
        v = ">" + v
    elif "=" in q:
        n, _, v = q.partition("=")
    else:
        n = q

    n = n.replace(":", "_")
    return n, v

def parse_arch_query(q):
    n, v = split_arch_name(q)

    return query(n + ":" + v)

def parse_arch_alias(q):
    n, v = split_arch_name(q)

    return name(name = n, version = v)

def convert_arch_package(ctx, fs, name):
    fs = filesystem(fs)

    ret = filesystem()

    for ent in fs:
        if ent.name.startswith("."):
            if ent.base == ".BUILDINFO":
                continue
            elif ent.base == ".MTREE":
                continue
            elif ent.base == ".PKGINFO":
                continue
            elif ent.base == ".INSTALL":
                ret[".pkg/{}/install".format(name)] = ent
            else:
                return error("name {} not implemented".format(ent.name))
        else:
            ret[ent.name] = ent

    return ctx.archive(ret)

def get_arch_installer(pkg, tags):
    ent = pkg.raw

    if tags.contains("level1"):
        return installer(
            directives = [
                directive.run_command("pacman -Sy --noconfirm {}".format(ent["name"])),
            ],
        )

    deps = [
        parse_arch_query(q)
        for q in ent["depends"].splitlines()
    ] if "depends" in ent else []

    if tags.contains("level2"):
        return installer(
            directives = [
                directive.run_command("pacman -Sy --noconfirm {}".format(ent["name"])),
            ],
            dependencies = deps,
        )
    elif tags.contains("level3"):
        return installer(
            tags = ["level3"],
            directives = [
                define.build(
                    convert_arch_package,
                    define.read_archive(
                        define.fetch_http(
                            ent["url_base"] + "/" + ent["filename"],
                        ),
                        ".tar.zst",
                    ),
                    ent["name"],
                ),
            ],
            dependencies = deps,
        )
    else:
        return None

def parse_arch_package(ctx, collection, packages):
    for ent in packages:
        aliases = [
            parse_arch_alias(k)
            for k in ent["provides"].splitlines()
        ] if "provides" in ent else []

        collection.add_package(
            name = name(
                name = ent["name"],
                version = ent["version"],
            ),
            aliases = aliases,
            raw = ent,
        )

def build_arch_directives(builder, plan):
    if plan.tags.contains("level3"):
        return plan.directives
    else:
        return [
            define.fetch_oci_image("library/archlinux"),
            directive.run_command("pacman -Sy"),
        ] + plan.directives

if __name__ == "__main__":
    db.add_container_builder(
        define.container_builder(
            name = "archlinux",
            arch = "x86_64",
            display_name = "Arch Linux",
            packages = define.package_collection(
                parse_arch_package,
                get_arch_installer,
                *[define.build(
                    parse_arch_database,
                    define.read_archive(
                        define.fetch_http(
                            "mirror://archlinux/{}/os/x86_64/{}.db.tar.gz".format(repo, repo),
                            expire_time = duration("8h"),
                        ),
                        ".tar.gz",
                    ),
                    "mirror://archlinux/{}/os/x86_64".format(repo),
                ) for repo in ["core", "community", "extra", "multilib"]]
            ),
            plan_callback = build_arch_directives,
        ),
    )

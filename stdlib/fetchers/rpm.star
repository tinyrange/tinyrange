db.add_mirror("fedora", ["https://mirror.aarnet.edu.au/pub/fedora/linux"])

def parse_rpm_database(ctx, base):
    repomd = db.build(define.fetch_http(
        base + "repodata/repomd.xml",
    )).read_xml()

    primary = [
        element
        for element in repomd["repomd"]["data"]
        if element["-type"] == "primary"
    ][0]

    primary_url = primary["location"]["-href"]

    primary = file(db.build(define.fetch_http(
        base + primary_url,
    )).read_compressed(primary_url))

    primary_xml = primary.read_rpm_xml()

    ret = ctx.recordwriter()

    for pkg in primary_xml:
        pkg["BaseUrl"] = base
        ret.emit(pkg)

    return ret

def parse_rpm_name_version(ent):
    if " if redhat-rpm-config)" in ent["Name"]:
        return None, None
    elif " if clang)" in ent["Name"]:
        return None, None
    elif " if gcc)" in ent["Name"]:
        return None, None

    ver = ent["Ver"]
    flags = ent["Flags"]
    if flags != "":
        if flags == "EQ":
            pass
        elif flags == "GE":
            ver = ">" + ver
        elif flags == "LE":
            ver = "<" + ver
        elif flags == "GT":
            ver = ">" + ver
        elif flags == "LT":
            ver = "<" + ver
        else:
            print("flags unhandled", flags, ver)

    return ent["Name"], ver

def parse_rpm_name(ent):
    n, v = parse_rpm_name_version(ent)

    if n == None:
        return None

    return name(name = n, version = v)

def parse_rpm_query(ent):
    n, v = parse_rpm_name_version(ent)

    if n == None:
        return None

    return query(n + ":" + v)

def convert_rpm_package(ctx, name, contents):
    fs = filesystem()

    rpm = contents.read_rpm()

    metadata = json.decode(rpm.metadata)

    fs[".pkg/{}/metadata.json".format(name)] = rpm.metadata

    if metadata["PreInstallScript"] != "":
        prg = " ".join(metadata["PreInstallScriptProgram"])

        if prg != "<lua>":
            fs[".pkg/{}/pre-install.sh".format(name)] = file("#!" + prg + "\n" + metadata["PreInstallScript"], executable = True)
        else:
            fs[".pkg/{}/pre-install.lua".format(name)] = file(metadata["PreInstallScript"], executable = True)
    if metadata["PostInstallScript"] != "":
        prg = " ".join(metadata["PostInstallScriptProgram"])

        if " ".join(metadata["PostInstallScriptProgram"]) != "<lua>":
            fs[".pkg/{}/post-install.sh".format(name)] = file("#!" + prg + "\n" + metadata["PostInstallScript"], executable = True)
        else:
            fs[".pkg/{}/post-install.lua".format(name)] = file(metadata["PostInstallScript"], executable = True)

    if rpm.payload_compression == "zstd":
        ark = db.build(define.read_archive(rpm.payload, ".cpio.zst"))

        for ent in filesystem(ark):
            fs[ent.name] = ent

        return ctx.archive(fs)
    else:
        return error("payload compression not implemented: " + rpm.payload_compression)

def get_rpm_installer(pkg, tags):
    ent = pkg.raw

    if tags.contains("level1"):
        return installer(
            directives = [
                directive.run_command("dnf install -y {}".format(ent["Name"])),
            ],
        )

    deps = []

    if ent["Format"]["Requires"]["Entry"] != None:
        deps = [parse_rpm_query(require) for require in ent["Format"]["Requires"]["Entry"]]

    deps = [f for f in deps if f != None]

    if tags.contains("level2"):
        return installer(
            directives = [
                directive.run_command("dnf install -y {}".format(ent["Name"])),
            ],
            dependencies = deps,
        )
    elif tags.contains("level3"):
        return installer(
            directives = [
                define.build(
                    convert_rpm_package,
                    ent["Name"],
                    define.fetch_http(ent["BaseUrl"] + ent["Location"]["Href"]),
                ),
            ],
            dependencies = deps,
        )
    else:
        return None

def parse_rpm_package(ctx, collection, packages):
    for ent in packages:
        aliases = []

        pkg_name = ent["Name"]
        pkg_version = ent["Version"]["Ver"]

        if ent["Format"]["Provides"]["Entry"] != None:
            aliases = aliases + [
                parse_rpm_name(provide)
                for provide in ent["Format"]["Provides"]["Entry"]
            ]

        if ent["Format"]["File"] != None:
            aliases = aliases + [
                name(name = file["Text"], version = pkg_version)
                for file in ent["Format"]["File"]
            ]

        aliases = [f for f in aliases if f != None]

        collection.add_package(
            name = name(
                name = pkg_name,
                version = pkg_version,
            ),
            aliases = aliases,
            raw = ent,
        )

def build_rpm_install_layer(ctx, directives):
    ret = filesystem()

    scripts = []

    for pkg in directives:
        if type(pkg) == "common.DirectiveRunCommand":
            continue

        res = ctx.build(pkg)

        fs = filesystem(res.read_archive())

        for ent in fs[".pkg"]:
            if "pre-install.sh" in ent:
                file = ent["pre-install.sh"]
                scripts.append({
                    "kind": "execute",
                    "exec": file.name,
                })

            if "post-install.sh" in ent:
                file = ent["post-install.sh"]
                scripts.append({
                    "kind": "execute",
                    "exec": file.name,
                })

    ret[".pkg/scripts.json"] = json.encode(scripts)

    return ctx.archive(ret)

def build_rpm_directives(builder, plan):
    if plan.tags.contains("level3"):
        return [
            define.build(
                build_rpm_install_layer,
                plan.directives,
            ),
        ] + plan.directives + [
            directive.run_command("/init -run-scripts /.pkg/scripts.json"),
        ]
    else:
        return [
            define.fetch_oci_image("library/fedora:40"),
        ] + plan.directives

if __name__ == "__main__":
    for version in ["40"]:
        db.add_container_builder(
            define.container_builder(
                name = "fedora@{}".format(version),
                display_name = "Fedora Linux {}".format(version),
                packages = define.package_collection(
                    parse_rpm_package,
                    get_rpm_installer,
                    define.build(
                        parse_rpm_database,
                        "mirror://fedora/releases/40/Everything/x86_64/os/",
                    ),
                    define.build(
                        parse_rpm_database,
                        "mirror://fedora/updates/40/Everything/x86_64/",
                    ),
                ),
                plan_callback = build_rpm_directives,
            ),
        )

db.add_mirror("ubuntu", ["https://mirror.aarnet.edu.au/pub/ubuntu/archive"])
db.add_mirror("neurodebian", ["https://mirror.aarnet.edu.au/pub/neurodebian"])
db.add_mirror("kali", ["https://mirror.aarnet.edu.au/pub/kali/kali"])

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
                    "{}/{}/binary-amd64/Packages.gz".format(base, component),
                    expire_time = duration("8h"),
                ),
                ".gz",
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

        if options[0] == "usrmerge":
            options = options[1:]

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
        elif rest.startswith("<="):
            pkg_version = "<" + rest.removeprefix("<= ")
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

def convert_debian_package(ctx, name, version, source):
    ar = db.build(define.read_archive(
        source,
        ".ar",
    ))

    data_ar = None
    control = None

    for f in ar:
        if f.name.startswith("data."):
            data_ar = db.build(define.read_archive(f, f.name))
        elif f.name.startswith("control."):
            control = filesystem(db.build(define.read_archive(f, f.name)))

    if data_ar == None or control == None:
        return error("could not find data and/or control")

    data = filesystem(data_ar)

    file_list = []

    for f in data_ar:
        filename = f.name.removeprefix(".")
        if filename == "/":
            filename = "/."
        file_list.append(filename)

    file_list.append("")

    ret = filesystem()

    for f in control:
        if f.base == ".":
            continue

        ret["var/lib/dpkg/info/{}.{}".format(name, f.base)] = f

    ret["var/lib/dpkg/info/{}.list".format(name)] = file("\n".join(file_list))

    ret[".pkg/{}".format(name)] = control
    ret[".pkg/{}/version".format(name)] = file(version)

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
                    ent["version"],
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
            tags = ["priority:" + ent["priority"].lower()],
            aliases = aliases,
            raw = ent,
        )

def make_ubuntu_repos(only_latest = True, include_neurodebian = False):
    ubuntu_repos = {}

    for version in ["jammy", "focal"]:
        if only_latest and version != LATEST_UBUNTU_VERSION:
            continue

        repos = []

        repos.append(define.build(
            parse_debian_release,
            define.fetch_http(
                url = "mirror://ubuntu/dists/{}/Release".format(version),
                expire_time = duration("8h"),
            ),
            "mirror://ubuntu/",
        ))

        ubuntu_repos[version] = define.package_collection(
            parse_debian_package,
            get_debian_installer,
            *repos
        )

        if include_neurodebian and version == "focal":
            repos.append(define.build(
                parse_debian_release,
                define.fetch_http(
                    url = "mirror://neurodebian/dists/{}/Release".format(version),
                    expire_time = duration("8h"),
                ),
                "mirror://neurodebian",
            ))

            ubuntu_repos[version + "_neurodebian"] = define.package_collection(
                parse_debian_package,
                get_debian_installer,
                *repos
            )

    return ubuntu_repos

def build_debian_install_layer(ctx, base_directives, directives):
    ret = filesystem()

    ret["usr/local/.keep"] = file("hello")

    preinst = []
    postinst = []

    status = ""

    for pkg in base_directives:
        if type(pkg) == "common.DirectiveRunCommand":
            continue

        fs = filesystem(pkg.read_archive())

        for ent in fs[".pkg"]:
            status += "\n".join([
                "Package: {}".format(ent.base),
                "Status: install ok installed",
                "Maintainer: TinyRange <tinyrange@tinyrange.dev>",
                "Version: {}".format(ent["version"].read()),
                "Architecture: amd64",
                "Description: TinyRange stub package",
                "",
                "",
            ])

    for pkg in directives:
        if type(pkg) == "common.DirectiveRunCommand":
            continue

        fs = filesystem(pkg.read_archive())

        for ent in fs[".pkg"]:
            if "preinst" in ent:
                f = ent["preinst"]
                preinst.append({
                    "kind": "execute",
                    "exec": f.name,
                    "args": ["install", ""],
                    "env": {
                        "PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
                        "DPKG_MAINTSCRIPT_PACKAGE": ent.base,
                        "DPKG_MAINTSCRIPT_NAME": "preinst",
                        "DPKG_ROOT": "",
                        "DEBIAN_FRONTEND": "noninteractive",
                        "DEBCONF_SYSTEMRC": "/root/.debconfrc",
                    },
                })

            if "postinst" in ent:
                f = ent["postinst"]
                postinst.append({
                    "kind": "execute",
                    "exec": f.name,
                    "args": ["configure", ""],
                    "env": {
                        "PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
                        "DPKG_MAINTSCRIPT_PACKAGE": ent.base,
                        "DPKG_MAINTSCRIPT_NAME": "postinst",
                        "DEBIAN_FRONTEND": "noninteractive",
                        "DEBCONF_SYSTEMRC": "/root/.debconfrc",
                    },
                })

            status += "\n".join([
                "Package: {}".format(ent.base),
                "Status: install ok installed",
                "Maintainer: TinyRange <tinyrange@tinyrange.dev>",
                "Version: {}".format(ent["version"].read()),
                "Architecture: amd64",
                "Description: TinyRange stub package",
                "",
                "",
            ])

    ret[".pkg/scripts.json"] = json.encode(preinst + postinst + [{
        "kind": "execute",
        "exec": "/bin/bash",
        "args": ["-c", "echo root:root | chpasswd"],
    }])
    ret["var/lib/dpkg/status"] = file(status)

    return ctx.archive(ret)

def build_debian_directives(builder, plan):
    if plan.tags.contains("level3"):
        directives = plan.base_directives + [
            directive.archive(define.build_vm(
                plan.base_directives + [
                    define.build(
                        build_debian_install_layer,
                        [],
                        plan.base_directives,
                    ),
                    directive.run_command("/init -run-scripts /.pkg/scripts.json"),
                ],
                output = "/init/changed.archive",
            )),
            define.build(
                build_debian_install_layer,
                plan.base_directives,
                plan.directives,
            ),
        ] + plan.directives

        if plan.tags.contains("noScripts"):
            return directives

        return directives + [
            directive.run_command("/init -run-scripts /.pkg/scripts.json"),
        ]
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
            arch = "x86_64",
            display_name = "Ubuntu " + version,
            plan_callback = build_debian_directives,
            # Packages with a high priority need to be installed.
            default_packages = [
                query("*", tags = ["priority:required"]),
                query("*", tags = ["priority:important"]),
                query("*", tags = ["priority:standard"]),
            ],
            split_default_packages = True,
            # This builder is scoped to just the packages in this repo.
            packages = repos[version],
            metadata = {
                "version": version,
            },
        ))

    return ret

if __name__ == "__main__":
    for builder in make_ubuntu_builders(make_ubuntu_repos(
        only_latest = False,
        include_neurodebian = True,
    )):
        db.add_container_builder(builder)

    db.add_container_builder(define.container_builder(
        name = "kali",
        arch = "x86_64",
        display_name = "Kali Linux",
        plan_callback = build_debian_directives,
        # Packages with a high priority need to be installed.
        default_packages = [
            query("usr-is-merged"),
            query("*", tags = ["priority:required"]),
            query("*", tags = ["priority:important"]),
            query("*", tags = ["priority:standard"]),
            query("kali-linux-core"),
            query("build-essential"),
        ],
        split_default_packages = True,
        # This builder is scoped to just the packages in this repo.
        packages = define.package_collection(
            parse_debian_package,
            get_debian_installer,
            define.build(
                parse_debian_release,
                define.fetch_http(
                    url = "mirror://kali/dists/kali-rolling/Release",
                    expire_time = duration("8h"),
                ),
                "mirror://kali/",
            ),
        ),
    ))

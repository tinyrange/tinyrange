db.add_mirror("ubuntu", ["https://mirror.aarnet.edu.au/pub/ubuntu/archive"])
db.add_mirror("neurodebian", ["https://mirror.aarnet.edu.au/pub/neurodebian"])
db.add_mirror("kali", ["https://mirror.aarnet.edu.au/pub/kali/kali"])

LATEST_UBUNTU_VERSION = "noble"
UBUNTU_VERSIONS = ["noble", "jammy", "focal"]

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

    for top in data:
        ret[top.name] = top

    for f in control:
        if f.base == ".":
            continue

        ret["var/lib/dpkg/info/{}.{}".format(name, f.base)] = f

    ret["var/lib/dpkg/info/{}.list".format(name)] = file("\n".join(file_list))

    ret[".pkg/{}".format(name)] = control
    ret[".pkg/{}/version".format(name)] = file(version)

    return ctx.archive(ret)

def is_redistributable(license):
    return True

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
                ).set_redistributable(is_redistributable("")),
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

    for version in UBUNTU_VERSIONS:
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

BASE_PASSWD = """root:*:0:0:root:/root:/bin/bash
daemon:*:1:1:daemon:/usr/sbin:/usr/sbin/nologin
bin:*:2:2:bin:/bin:/usr/sbin/nologin
sys:*:3:3:sys:/dev:/usr/sbin/nologin
sync:*:4:65534:sync:/bin:/bin/sync
games:*:5:60:games:/usr/games:/usr/sbin/nologin
man:*:6:12:man:/var/cache/man:/usr/sbin/nologin
lp:*:7:7:lp:/var/spool/lpd:/usr/sbin/nologin
mail:*:8:8:mail:/var/mail:/usr/sbin/nologin
news:*:9:9:news:/var/spool/news:/usr/sbin/nologin
uucp:*:10:10:uucp:/var/spool/uucp:/usr/sbin/nologin
proxy:*:13:13:proxy:/bin:/usr/sbin/nologin
www-data:*:33:33:www-data:/var/www:/usr/sbin/nologin
backup:*:34:34:backup:/var/backups:/usr/sbin/nologin
list:*:38:38:Mailing List Manager:/var/list:/usr/sbin/nologin
irc:*:39:39:ircd:/run/ircd:/usr/sbin/nologin
_apt:*:42:65534::/nonexistent:/usr/sbin/nologin
nobody:*:65534:65534:nobody:/nonexistent:/usr/sbin/nologin"""

BASE_GROUP = """root:*:0:
daemon:*:1:
bin:*:2:
sys:*:3:
adm:*:4:
tty:*:5:
disk:*:6:
lp:*:7:
mail:*:8:
news:*:9:
uucp:*:10:
man:*:12:
proxy:*:13:
kmem:*:15:
dialout:*:20:
fax:*:21:
voice:*:22:
cdrom:*:24:
floppy:*:25:
tape:*:26:
sudo:*:27:
audio:*:29:
dip:*:30:
www-data:*:33:
backup:*:34:
operator:*:37:
list:*:38:
irc:*:39:
src:*:40:
shadow:*:42:
utmp:*:43:
video:*:44:
sasl:*:45:
plugdev:*:46:
staff:*:50:
games:*:60:
users:*:100:
nogroup:*:65534:"""

def build_debian_install_layer(ctx, base_directives, directives):
    ret = filesystem()

    status = ""

    for pkg in base_directives:
        if type(pkg) == "common.DirectiveRunCommand":
            continue

        fs = filesystem(pkg.read_archive())

        for ent in fs[".pkg"]:
            control = ent["control"].read().strip()
            status += control + "\nStatus: install ok installed\n\n"

    for pkg in directives:
        if type(pkg) == "common.DirectiveRunCommand":
            continue

        fs = filesystem(pkg.read_archive())

        for ent in fs[".pkg"]:
            control = ent["control"].read().strip()
            status += control + "\nStatus: install ok unpacked\n\n"

    ret[".pkg/scripts.json"] = json.encode([
        {
            "kind": "execute",
            "exec": "/usr/bin/dpkg",
            "args": ["--configure", "--pending"],
            "env": {
                "PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
                "DEBIAN_FRONTEND": "noninteractive",
            },
        },
        {
            "kind": "execute",
            "exec": "/bin/bash",
            "args": ["-c", "echo root:root | chpasswd"],
        },
    ])

    ret["var/lib/dpkg/status"] = file(status)
    ret["var/log/dpkg.log"] = file("")
    ret["usr/local"] = filesystem()
    ret["etc/passwd"] = file(BASE_PASSWD)
    ret["etc/group"] = file(BASE_GROUP)

    return ctx.archive(ret)

def build_debian_directives(builder, plan):
    if plan.tags.contains("level3"):
        if plan.tags.contains("slowBoot"):
            directives = plan.base_directives + [
                define.build(
                    build_debian_install_layer,
                    [],
                    plan.base_directives + plan.directives,
                ),
            ] + plan.directives
        else:
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
            directive.default_interactive("/bin/login -f root"),
            directive.run_command("/init -run-scripts /.pkg/scripts.json"),
        ]
    else:
        return [
            define.fetch_oci_image(image = "library/ubuntu", tag = builder.metadata["version"]),
        ] + plan.directives

def make_ubuntu_builders(repos):
    ret = []
    for version in repos:
        defaults = [
            query("cdebconf"),
            query("dpkg"),
            query("*", tags = ["priority:required"]),
            query("*", tags = ["priority:important"]),
            query("*", tags = ["priority:standard"]),
        ]

        if version == "noble":
            defaults = [
                query("usr-is-merged"),
            ] + defaults

        # Define a container builder for each version.
        ret.append(define.container_builder(
            name = "ubuntu@" + version,
            arch = "x86_64",
            display_name = "Ubuntu " + version,
            plan_callback = build_debian_directives,
            # Packages with a high priority need to be installed.
            default_packages = defaults,
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

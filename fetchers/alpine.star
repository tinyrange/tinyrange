db.add_mirror("alpine", ["https://dl-cdn.alpinelinux.org/alpine"])

LATEST_ALPINE_VERSION = "3.20"

ALPINE_VERSIONS = [
    "edge",
    "3.20",
    "3.19",
    "3.18",
    "3.17",
    "3.16",
    "3.15",
    "3.14",
    "3.13",
    "3.12",
    "3.11",
    "3.10",
    "3.9",
    "3.8",
    "3.7",
    "3.6",
    "3.5",
    "3.4",
    "3.3",
    "3.2",
    "3.1",
    "3.0",
]

def parse_alpine_repo(ctx, index, url_base):
    index = index["APKINDEX"]

    # Create a record writer which writes starlark objects to a file.
    ret = ctx.recordwriter()

    pkg = {"url_base": url_base}

    for line in index.read().split("\n"):
        if len(line) == 0:
            if len(pkg) > 1:
                ret.emit(pkg)
            pkg = {"url_base": url_base}
        else:
            k, v = line.split(":", 1)
            pkg[k] = v

    if len(pkg) > 1:
        ret.emit(pkg)

    return ret

def split_alpine_name(q):
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

def parse_alpine_query(q):
    n, v = split_alpine_name(q)

    return query(n + ":" + v)

def parse_alpine_alias(q):
    n, v = split_alpine_name(q)

    return name(name = n, version = v)

def parse_alpine_pkginfo(contents):
    ret = {}
    for line in contents.split("\n"):
        if line.startswith("#"):
            continue
        k, _, v = line.partition(" = ")
        if k not in ret:
            ret[k] = []
        ret[k].append(v)
    return ret

def apk_download(ctx, name, fs):
    fs = filesystem(fs)

    ret = filesystem()

    info = parse_alpine_pkginfo(fs[".PKGINFO"].read())

    for ent in fs:
        if ent.name.startswith("."):
            if ent.name.startswith(".SIGN."):
                continue
            elif ent.name == ".PKGINFO":
                ret[".pkg/{}/info".format(name)] = ent
            elif ent.name == ".dummy":
                continue
            elif ent.name == ".pre-install":
                ret[".pkg/{}/pre-install.sh".format(name)] = ent
            elif ent.name == ".post-install":
                ret[".pkg/{}/post-install.sh".format(name)] = ent
            elif ent.name == ".pre-upgrade":
                continue  # We only install packages.
            elif ent.name == ".post-upgrade":
                continue  # We only install packages.
            elif ent.name == ".pre-deinstall":
                continue
            elif ent.name == ".trigger":
                triggers = info["triggers"][0].split(" ")
                ret[".pkg/{}/trigger.sh".format(name)] = ent
                ret[".pkg/{}/trigger.json".format(name)] = json.encode(triggers)
            else:
                return error("name {} not implemented".format(ent.name))
        else:
            ret[ent.name] = ent

    return ctx.archive(ret)

def parse_alpine_packages(ctx, ent):
    deps = [parse_alpine_query(dep) for dep in ent["D"].split(" ")] if "D" in ent else []

    download_archive = define.read_archive(
        define.fetch_http(
            "{}/{}-{}.apk".format(ent["url_base"], ent["P"], ent["V"]),
        ),
        ".tar.gz",
    )

    ctx.add_package(package(
        name = name(
            name = ent["P"].replace(":", "_"),
            version = ent["V"],
        ),
        installers = [
            installer(
                tags = ["level1"],
                # This is a basic package defintion that just uses apk to install the package.
                directives = [
                    directive.run_command("apk add {}".format(ent["P"])),
                ],
            ),
            installer(
                tags = ["level2"],
                directives = [
                    directive.run_command("apk add {}".format(ent["P"])),
                ],
                dependencies = deps,
            ),
            installer(
                tags = ["level3"],
                directives = [
                    define.build(
                        apk_download,
                        ent["P"],
                        download_archive,
                    ),
                ],
                dependencies = deps,
            ),
        ],
        aliases = [parse_alpine_alias(provides) for provides in ent["p"].split(" ")] if "p" in ent else [],
        raw = ent,
    ))

def make_alpine_repos(only_latest = True):
    alpine_repos = {}

    for version in ALPINE_VERSIONS:
        if only_latest and version != LATEST_ALPINE_VERSION:
            continue

        repos = []

        repo_list = ["main"]

        server_version = version

        if version == "edge":
            repo_list.append("community")
            repo_list.append("testing")
        else:
            server_version = "v" + server_version
            if int(version.split(".")[1]) > 3:
                repo_list.append("community")

        for repo in repo_list:
            repos.append(define.build(
                parse_alpine_repo,
                define.read_archive(
                    define.fetch_http(
                        "mirror://alpine/{}/{}/{}/APKINDEX.tar.gz".format(server_version, repo, "x86_64"),
                        expire_time = duration("8h"),
                    ),
                    ".tar.gz",
                ),
                "mirror://alpine/{}/{}/{}".format(server_version, repo, "x86_64"),
            ))

        # Define a package collection containing all the repos.
        alpine_repos[version] = define.package_collection(
            parse_alpine_packages,
            *repos
        )

    return alpine_repos

def build_alpine_install_layer(ctx, directives):
    ret = filesystem()

    scripts = []

    for pkg in directives:
        if type(pkg) == "common.DirectiveRunCommand":
            continue

        res = ctx.build(pkg)

        fs = filesystem(res.read_archive())

        for ent in fs[".pkg"]:
            if "post-install.sh" in ent:
                file = ent["post-install.sh"]
                scripts.append({
                    "kind": "execute",
                    "exec": file.name,
                })

            if "pre-install.sh" in ent:
                file = ent["pre-install.sh"]
                scripts.append({
                    "kind": "execute",
                    "exec": file.name,
                })

            if "trigger.json" in ent:
                file = ent["trigger.json"]
                triggers = json.decode(file.read())

                scripts.append({
                    "kind": "trigger_on",
                    "triggers": triggers,
                    "exec": file.name.removesuffix(".json") + ".sh",
                })

    ret[".pkg/scripts.json"] = json.encode(scripts)

    return ctx.archive(ret)

def build_alpine_directives(builder, plan):
    if plan.tags.contains("level3"):
        # If we are using level3 then add a first layer that generates the inital scripts.

        return [
            define.build(
                build_alpine_install_layer,
                plan.directives,
            ),
        ] + plan.directives + [
            directive.run_command("/builder -runScripts /.pkg/scripts.json"),
        ]
    else:
        # If we are level1 or level2 then just make sure we have the normal base image.
        return [
            define.fetch_oci_image(
                image = "library/alpine",
                version = builder.metadata["version"],
            ),
        ] + plan.directives

def make_alpine_builders(repos):
    ret = []
    for version in repos:
        # Define a container builder for each version.
        ret.append(define.container_builder(
            name = "alpine@" + version,
            display_name = "Alpine " + version,

            # Specify a plan callback to add the initial layer.
            # The plan callback just returns the list of directives after the plan is created.
            plan_callback = build_alpine_directives,

            # This builder is scoped to just the packages in this repo.
            packages = repos[version],

            # Make the alpine version avalible to the plan_callback.
            metadata = {
                "version": version,
            },
        ))

    return ret

if __name__ == "__main__":
    for builder in make_alpine_builders(make_alpine_repos(only_latest = False)):
        db.add_container_builder(builder)

load("common/docker.star", "build_docker_archive_from_layers")
load("fetchers/neurocontainers.star", "fetch_neurocontainers_repository")
load("repos/rpm.star", "add_fedora_fetchers")

def build_merged_install_layer_from_plan(ctx, plan):
    build_script = "#!/bin/sh\n"

    layer_fs = filesystem()

    for f in plan:
        fs = filesystem(f.read_archive(".tar"))

        metadata = None

        if ".pkg/rpm/metadata" in fs:
            metadata = json.decode([f for f in fs[".pkg/rpm/metadata"]][0].read())
        else:
            return error("layer has no metadata")

        if ".pkg/rpm/pre-install" in fs:
            prog = " ".join(metadata["PreInstallScriptProgram"])
            if prog == "<lua>":
                build_script += "\n".join(["# run-rpm-lua " + f.name for f in fs[".pkg/rpm/pre-install"]]) + "\n"
            elif prog == None or prog == "/bin/sh":
                build_script += "\n".join([f.name for f in fs[".pkg/rpm/pre-install"]]) + "\n"
            else:
                return error("prog (" + prog + ") not implemented")

        if ".pkg/rpm/post-install" in fs:
            prog = " ".join(metadata["PostInstallScriptProgram"])
            if prog == "<lua>":
                build_script += "\n".join(["# run-rpm-lua " + f.name for f in fs[".pkg/rpm/post-install"]]) + "\n"
            elif prog == None or prog == "/bin/sh":
                build_script += "\n".join([f.name for f in fs[".pkg/rpm/post-install"]]) + "\n"
            else:
                return error("prog (" + prog + ") not implemented")

        layer_fs += fs

    layer_fs[".pkg/install.sh"] = file(build_script, executable = True)

    return ctx.archive(layer_fs)

def main(ctx):
    if len(ctx.args) == 0:
        print("usage: make_docker_image.star <package_name>")
        return None

    container_name = ctx.args[0]

    result = ctx.query(name(container_name, distribution = "neurocontainers"))
    if len(result) == 0:
        print("package not found")
        return None

    pkg = result[0]

    directives = json.decode(pkg.raw)

    arch = "x86_64"

    base_image = ""

    incremental_plan = ctx.incremental_plan(
        prefer_architecture = arch,
    )

    layers = []

    for directive in directives:
        kind = directive[0]
        if kind == "base-image":
            if directive[1] == "fedora:35":
                base_image = "fedora@35"
            else:
                return error("base image not implemented: " + directive[1])
        elif kind == "pkg-manager":
            pass
        elif kind == "run":
            script = directive[1]
            print("run not implemented", script)
        elif kind == "install":
            pkg_name = directive[1]
            pkgs = incremental_plan.add(name(
                name = pkg_name,
                distribution = base_image,
                architecture = arch,
            ))
            layers += [ctx.download_def(pkg) for pkg in pkgs]
        elif kind == "env":
            k, _, v = directive[1].partition("=")
            print("env not implemented", k, v)
        elif kind == "copy":
            _, source, destination = directive
            print("copy not implemented", source, destination)
        else:
            return error("directive not implemented: " + kind)

    layer = build_def(
        (__file__, "neurocontainers", container_name),
        build_merged_install_layer_from_plan,
        (layers,),
    )

    combined_rootfs = ctx.build(
        (__file__, "neurocontainers", container_name, "rootfs"),
        build_docker_archive_from_layers,
        (
            "neurocontainers/" + ctx.args[0] + ":latest",
            [layer],
        ),
    )

    # incremental_plan.dump_graph()

    print(get_cache_filename(combined_rootfs))

    return None

if __name__ == "__main__":
    fetch_repo(
        fetch_neurocontainers_repository,
        ("https://github.com/NeuroDesk/neurocontainers", "master"),
        distro = "neurocontainers",
    )
    add_fedora_fetchers(only_version = "35")

    run_script(main)

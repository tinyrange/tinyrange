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
        return

    container_name = ctx.args[0]

    result = ctx.query(name(container_name, distribution = "neurocontainers"))
    if len(result) == 0:
        print("package not found")
        return

    pkg = result[0]

    builder = json.decode(pkg.builders[0].json_string)

    depends = []

    for directive in builder["Directives"]:
        # TODO(joshua): Handle other directives.
        if directive["Kind"] == "Dependency":
            depends.append(name(
                name = directive["Name"]["Name"],
                distribution = "fedora@35",
                architecture = "x86_64",
            ))

    print("generating plan for", container_name)

    plan = ctx.plan(prefer_architecture = "x86_64", *depends)

    layers = [ctx.download_def(pkg) for pkg in plan]

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

    print(get_cache_filename(combined_rootfs))

if __name__ == "__main__":
    fetch_repo(
        fetch_neurocontainers_repository,
        ("https://github.com/NeuroDesk/neurocontainers", "master"),
        distro = "neurocontainers",
    )
    add_fedora_fetchers(only_version = "35")

    run_script(main)

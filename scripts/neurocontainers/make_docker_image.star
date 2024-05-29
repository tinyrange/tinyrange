load("common/docker.star", "build_docker_archive_from_layers", "make_fs")
load("fetchers/neurocontainers.star", "fetch_neurocontainers_repository")
load("repos/rpm.star", "add_fedora_fetchers")

def build_install_layer_from_plan(ctx, plan):
    build_script = "#!/bin/sh\n"

    triggers = []

    total_fs = filesystem()

    layers = []

    for f in plan:
        fs = make_fs(f.read_archive(".tar"))

        layers.append(f.hash("sha256"))

        total_fs += fs

        if ".pkg/rpm/pre-install" in fs:
            build_script += "\n".join([f.name for f in fs[".pkg/rpm/pre-install"]]) + "\n"

        if ".pkg/rpm/post-install" in fs:
            build_script += "\n".join([f.name for f in fs[".pkg/rpm/post-install"]]) + "\n"

    for triggers, script in triggers:
        for trigger in triggers:
            if trigger in total_fs:
                build_script += "{} {}\n".format(script, trigger)

    layer_fs = filesystem()

    layer_fs[".pkg/install.sh"] = file(build_script, executable = True)
    layer_fs[".pkg/layers.json"] = json.encode(layers)

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

    final_layer = build_def(
        (__file__, "neurocontainers", container_name),
        build_install_layer_from_plan,
        (layers,),
    )

    combined_rootfs = ctx.build(
        (__file__, "neurocontainers", container_name, "rootfs"),
        build_docker_archive_from_layers,
        (
            "neurocontainers/" + ctx.args[0] + ":latest",
            layers + [final_layer],
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

load("common/docker.star", "build_docker_archive_from_layers")
load("repos/alpine.star", "add_alpine_fetchers")

def build_install_layer_from_plan(ctx, plan):
    # Generate a install layer for a collection of Alpine layers.

    build_script = "#!/bin/sh\n"

    triggers = []

    total_fs = filesystem()

    for f in plan:
        # Read the layer filesystem.
        fs = filesystem(f.read_archive(".tar"))

        # If this doesn't have APK metadata then ignore it.
        if ".pkg/apk" not in fs:
            continue

        # Combine all layers into a final filesystem.
        total_fs += fs

        # Look for any pre-install scripts in the layer.
        if ".pkg/apk/pre-install" in fs:
            build_script += "\n".join([f.name for f in fs[".pkg/apk/pre-install"]]) + "\n"

        # Look for any post-install scripts in the layer.
        if ".pkg/apk/post-install" in fs:
            build_script += "\n".join([f.name for f in fs[".pkg/apk/post-install"]]) + "\n"

        if ".pkg/apk/trigger" in fs:
            # Look for the trigger/name.sh and trigger/name.txt files.
            names = [f for f in fs[".pkg/apk/trigger"]]
            sh = [f for f in names if f.name.endswith(".sh")][0]
            txt = [f for f in names if f.name.endswith(".txt")][0]
            triggers.append((json.decode(txt.read()), sh.name))

    # Check for any triggers that need executing.
    for triggers, script in triggers:
        for trigger in triggers:
            # Check if the trigger exists.
            if trigger in total_fs:
                # Triggers are passed the directory name.
                build_script += "{} {}\n".format(script, trigger)

    # Create the final layer filesystem.
    # This is the only part that gets written in the end.
    layer_fs = filesystem()

    layer_fs[".pkg/install.sh"] = file(build_script, executable = True)

    return ctx.archive(layer_fs)

def main(ctx):
    # Make a plan for each of the packages.
    plan = ctx.plan(
        name("build-base"),
        name("alpine-baselayout"),
        name("busybox"),
    )

    # Create a layer for each of the packages.
    layers = [ctx.download_def(pkg) for pkg in plan]

    # Create the final layer.
    final_layer = build_def(
        (__file__, plan),
        build_install_layer_from_plan,
        (layers,),
    )

    # Create a combined_rootfs.
    combined_rootfs = ctx.build(
        (__file__, plan, "rootfs"),
        build_docker_archive_from_layers,
        ("pkg2/gcc:latest", layers + [final_layer]),
    )

    # Print the final filename of the archive.
    # This archive can be loaded with Docker.
    print(get_cache_filename(combined_rootfs))

if __name__ == "__main__":
    add_alpine_fetchers(only_latest = True)
    run_script(main)

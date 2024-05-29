load("common/docker.star", "build_docker_archive_from_layers", "make_fs")
load("repos/alpine.star", "add_alpine_fetchers")

def parse_pkg_info(contents):
    ret = {}

    for line in contents.splitlines():
        if line.startswith("#"):
            continue
        k, v = line.split(" = ", 1)
        ret[k] = v

    return ret

def build_install_layer_from_plan(ctx, plan):
    build_script = "#!/bin/sh\n"

    triggers = []

    total_fs = filesystem()

    layers = []

    for f in plan:
        fs = make_fs(f.read_archive(".tar"))

        layers.append(f.hash("sha256"))

        total_fs += fs

        if ".pkg/apk/pre-install" in fs:
            build_script += "\n".join([f.name for f in fs[".pkg/apk/pre-install"]]) + "\n"

        if ".pkg/apk/post-install" in fs:
            build_script += "\n".join([f.name for f in fs[".pkg/apk/post-install"]]) + "\n"

        if ".pkg/apk/trigger" in fs:
            names = [f for f in fs[".pkg/apk/trigger"]]
            sh = [f for f in names if f.name.endswith(".sh")][0]
            txt = [f for f in names if f.name.endswith(".txt")][0]
            triggers.append((json.decode(txt.read()), sh.name))

    for triggers, script in triggers:
        for trigger in triggers:
            if trigger in total_fs:
                build_script += "{} {}\n".format(script, trigger)

    layer_fs = filesystem()

    layer_fs[".pkg/install.sh"] = file(build_script, executable = True)
    layer_fs[".pkg/layers.json"] = json.encode(layers)

    return ctx.archive(layer_fs)

def main(ctx):
    plan = ctx.plan(name("build-base"), name("alpine-baselayout"), name("busybox"))

    layers = [ctx.download_def(pkg) for pkg in plan]

    final_layer = build_def((__file__, plan), build_install_layer_from_plan, (layers,))

    combined_rootfs = ctx.build((__file__, plan, "rootfs"), build_docker_archive_from_layers, ("pkg2/gcc:latest", layers + [final_layer]))

    print(get_cache_filename(combined_rootfs))

if __name__ == "__main__":
    add_alpine_fetchers(only_latest = True)
    run_script(main)

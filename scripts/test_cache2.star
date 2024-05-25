load("repos/alpine.star", "add_alpine_fetchers")

def make_fs(ark):
    fs = filesystem()

    for file in ark:
        if file.name.endswith("/"):
            continue
        fs[file.name] = file

    return fs

def parse_pkg_info(contents):
    ret = {}

    for line in contents.splitlines():
        if line.startswith("#"):
            continue
        k, v = line.split(" = ", 1)
        ret[k] = v

    return ret

def build_package_archive(ctx, pkg):
    fs = filesystem()

    ark = ctx.db.download(pkg)

    pkg_info = None

    for file in ark:
        if file.name.startswith("."):
            if file.name == ".PKGINFO":
                pkg_info = parse_pkg_info(file.read())
                continue
            elif file.name.startswith(".SIGN.RSA"):
                # TODO(joshua): Validate signatures.
                continue
            elif file.name == ".pre-install":
                fs[".pkg/apk/pre-install/" + pkg.name + ".sh"] = file
                continue
            elif file.name == ".post-install":
                fs[".pkg/apk/post-install/" + pkg.name + ".sh"] = file
                continue
            elif file.name == ".pre-upgrade":
                # Not needed since we are always installing not upgrading.
                continue
            elif file.name == ".post-upgrade":
                # Not needed since we are always installing not upgrading.
                continue
            elif file.name == ".trigger":
                fs[".pkg/apk/trigger/" + pkg.name + ".txt"] = json.encode(pkg_info["triggers"].split(" "))
                fs[".pkg/apk/trigger/" + pkg.name + ".sh"] = file
                continue
            elif file.name == ".dummy":
                continue
            else:
                return error("hidden file not implemented: " + file.name)
        if file.name.endswith("/"):
            continue  # ignore directoriess
        fs[file.name] = file

    return ctx.archive(fs)

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

def concat_layers(ctx, layers):
    fs = filesystem()

    for layer in layers:
        fs += make_fs(layer.read_archive(".tar"))

    return ctx.archive(fs)

def main(ctx):
    plan = ctx.plan(name("build-base"), name("alpine-baselayout"), name("busybox"))

    layers = [build_def((__file__, pkg), build_package_archive, (pkg,)) for pkg in plan]

    final_layer = build_def((__file__, plan), build_install_layer_from_plan, (layers,))

    combined_rootfs = ctx.build((__file__, plan, "rootfs"), concat_layers, (layers + [final_layer],))

    print(get_cache_filename(combined_rootfs))

if __name__ == "__main__":
    add_alpine_fetchers(only_latest = True)
    run_script(main)

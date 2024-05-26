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

def tar_of_tars(ctx, layers):
    fs = filesystem()

    layer_info_list = []
    layer_list = []
    layer_sources = {}
    layer_digests = []

    for layer in layers:
        layer_filename = "blobs/sha256/" + layer.hash("sha256")
        fs[layer_filename] = layer
        layer_digest = "sha256:" + layer.hash("sha256")
        layer_info = {
            "mediaType": "application/vnd.oci.image.layer.v1.tar",
            "size": layer.size,
            "digest": layer_digest,
        }
        layer_info_list.append(layer_info)
        layer_list.append(layer_filename)
        layer_sources[layer_filename] = layer_info
        layer_digests.append(layer_digest)

    config = file(json.encode({
        "architecture": "amd64",
        "config": {
            "Hostname": "",
            "Domainname": "",
            "User": "",
            "AttachStdin": False,
            "AttachStdout": False,
            "AttachStderr": False,
            "Tty": False,
            "OpenStdin": False,
            "StdinOnce": False,
            "Env": ["PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"],
            "Cmd": ["/bin/sh"],
            "Volumes": None,
            "WorkingDir": "",
            "Entrypoint": None,
            "OnBuild": None,
            "Labels": None,
        },
        "history": [],
        "os": "linux",
        "rootfs": {
            "type": "layers",
            "diff_ids": layer_digests,
        },
    }))

    manifest = file(json.encode({
        "schemaVersion": 2,
        "mediaType": "application/vnd.oci.image.manifest.v1+json",
        "config": {
            "mediaType": "application/vnd.oci.image.config.v1+json",
            "digest": "sha256:" + config.hash("sha256"),
            "size": config.size,
        },
        "layers": layer_info_list,
    }))

    config_path = "blobs/sha256/" + config.hash("sha256")
    fs[config_path] = config

    manifest_path = "blobs/sha256/" + manifest.hash("sha256")
    fs[manifest_path] = manifest

    fs["oci-layout"] = json.encode({
        "imageLayoutVersion": "1.0.0",
    })

    fs["index.json"] = json.encode({
        "schemaVersion": 2,
        "mediaType": "application/vnd.oci.image.index.v1+json",
        "manifests": [
            {
                "mediaType": "application/vnd.oci.image.manifest.v1+json",
                "digest": "sha256:" + manifest.hash("sha256"),
                "size": manifest.size,
                "annotations": {},
            },
        ],
    })

    fs["manifest.json"] = json.encode([
        {
            "Config": config_path,
            "RepoTags": ["pkg2/gcc:latest"],
            "Layers": layer_list,
            "LayerSources": layer_sources,
        },
    ])

    return ctx.archive(fs)

def main(ctx):
    plan = ctx.plan(name("build-base"), name("alpine-baselayout"), name("busybox"))

    layers = [build_def((__file__, pkg), build_package_archive, (pkg,)) for pkg in plan]

    final_layer = build_def((__file__, plan), build_install_layer_from_plan, (layers,))

    combined_rootfs = ctx.build((__file__, plan, "rootfs"), tar_of_tars, (layers + [final_layer],))

    print(get_cache_filename(combined_rootfs))

if __name__ == "__main__":
    add_alpine_fetchers(only_latest = True)
    run_script(main)

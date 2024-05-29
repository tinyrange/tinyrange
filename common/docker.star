def build_docker_archive_from_layers(ctx, name, layers):
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
            "RepoTags": [name],
            "Layers": layer_list,
            "LayerSources": layer_sources,
        },
    ])

    return ctx.archive(fs)

def make_fs(ark):
    fs = filesystem()

    for file in ark:
        if file.name.endswith("/"):
            continue
        fs[file.name] = file

    return fs

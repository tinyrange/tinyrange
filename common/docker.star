def build_docker_archive_from_layers(ctx, name, layers):
    fs = filesystem()

    layer_info_list = []
    layer_list = []
    layer_sources = {}
    layer_digests = []

    # Layers is an array of files.
    for layer in layers:
        # Add the layer to the final filesystem.
        layer_filename = "blobs/sha256/" + layer.hash("sha256")
        fs[layer_filename] = layer

        # Generate the layer digest.
        layer_digest = "sha256:" + layer.hash("sha256")

        # Generate the layer info.
        layer_info = {
            "mediaType": "application/vnd.oci.image.layer.v1.tar",
            "size": layer.size,
            "digest": layer_digest,
        }

        # Add the layer metadata to the structures for later.
        layer_info_list.append(layer_info)
        layer_list.append(layer_filename)
        layer_sources[layer_filename] = layer_info
        layer_digests.append(layer_digest)

    config = file(json.encode({
        # TODO(joshua): Currently amd64 is hardcoded.
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
            # TODO(joshua): This should be customisable.
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

    # Create the manifest file.
    # This points to the config and each layer.
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

    # Write the config file to the image.
    config_path = "blobs/sha256/" + config.hash("sha256")
    fs[config_path] = config

    # Write the manifest to the image.
    manifest_path = "blobs/sha256/" + manifest.hash("sha256")
    fs[manifest_path] = manifest

    fs["oci-layout"] = json.encode({
        "imageLayoutVersion": "1.0.0",
    })

    # Write the index. This just points to the image manifest.
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

    # Write the final image manifest.
    fs["manifest.json"] = json.encode([
        {
            "Config": config_path,
            "RepoTags": [name],
            "Layers": layer_list,
            "LayerSources": layer_sources,
        },
    ])

    return ctx.archive(fs)

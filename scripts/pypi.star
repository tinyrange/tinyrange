db.add_mirror("pypi", ["https://pypi.org/simple"])

def parse_filename(filename):
    if filename.endswith(".whl"):
        filename = filename.removesuffix(".whl")

        name, _, rest = filename.partition("-")
        version, _, abi = rest.partition("-")

        return {
            "wheel": True,
            "name": name,
            "version": version,
            "abi": abi.split("-"),
        }
    elif filename.endswith(".tar.gz"):
        filename = filename.removesuffix(".tar.gz")

        name, _, version = filename.rpartition("-")

        return {
            "wheel": False,
            "name": name,
            "version": version,
        }
    elif filename.endswith(".zip"):
        filename = filename.removesuffix(".zip")

        name, _, version = filename.rpartition("-")

        return {
            "wheel": False,
            "name": name,
            "version": version,
        }
    elif filename.endswith(".tar.bz2"):
        filename = filename.removesuffix(".tar.bz2")

        name, _, version = filename.rpartition("-")

        return {
            "wheel": False,
            "name": name,
            "version": version,
        }
    elif filename.endswith(".egg"):
        return {"egg": True}
    elif filename.endswith(".exe"):
        return {"windows": True}
    else:
        return error("{} not implemented".format(filename))

def satisfies_version(pkg_version, target_version):
    target_version = target_version.strip()

    if target_version == "":
        return True

    if target_version.startswith("(") and target_version.endswith(")"):
        target_version = target_version[1:-1]

    if target_version.startswith(">="):
        target_version = target_version.removeprefix(">=")
        return pkg_version >= target_version
    elif target_version.startswith(">"):
        target_version = target_version.removeprefix(">")
        return pkg_version > target_version
    else:
        return error("satisfies_version not implemented: {} {}".format(pkg_version, target_version))

def satisfies_wheel(f, abi_set):
    interpreter, abi, platform = f["abi"]

    for interpreter_option in interpreter.split("."):
        if interpreter_option not in abi_set:
            continue

        interpreter_set = abi_set[interpreter_option]

        for abi_option in abi.split("."):
            if abi_option not in interpreter_set:
                continue

            abi_set = interpreter_set[abi_option]

            for platform_option in platform.split("."):
                if platform_option in abi_set:
                    return True

    return False

def get_python_abi(base):
    return define.build_vm(
        [
            base,
            directive.add_file("/tags.py", file("""import packaging.tags
import json
import sys

lst = [[tag.interpreter, tag.abi, tag.platform] for tag in packaging.tags.sys_tags()]

ret = {
  "tags": {},
  "version": ".".join([
    str(sys.version_info.major),
    str(sys.version_info.minor),
    str(sys.version_info.micro),
  ]),
  "platform": sys.platform,
}

for interpreter, abi, platform in lst:
  if interpreter not in ret["tags"]:
    ret["tags"][interpreter] = {}
  
  if abi not in ret["tags"][interpreter]:
    ret["tags"][interpreter][abi] = {}
  
  ret["tags"][interpreter][abi][platform] = True

print(json.dumps(ret))
""")),
            directive.run_command("python3 /tags.py > /tags.json"),
        ],
        output = "/tags.json",
    )

def parse_python_metadata(contents):
    lines = contents.splitlines()

    ret = {}
    last_key = ""

    for line in lines:
        line = line.strip("\r\n")

        if line == "":
            break
        elif line.startswith(" ") or line.startswith("\t"):
            ret[last_key].append(line)
        elif ": " in line:
            key, value = line.split(": ", 1)
            if key not in ret:
                ret[key] = []
            ret[key].append(value)
            last_key = key
        elif line.endswith(":"):
            key = line.removesuffix(":")
            if key not in ret:
                ret[key] = []
            ret[key].append("")
            last_key = key
        else:
            ret[last_key].append(line)

    return ret

def _get_metadata(ctx, name, version, ark):
    fs = filesystem(ark)

    dist_info_name = "{}-{}.dist-info".format(name, version)

    if dist_info_name in fs:
        dist_info = fs[dist_info_name]

        metadata = parse_python_metadata(dist_info["METADATA"].read())

        return file(json.encode(metadata))
    elif "{}-{}".format(name, version) in fs:
        top = fs["{}-{}".format(name, version)]
        if "PKG-INFO" in top:
            metadata = parse_python_metadata(top["PKG-INFO"].read())

            return file(json.encode(metadata))
        else:
            print([f for f in top])
            return error("could not find metadata in top")
    else:
        print([f for f in fs])
        return error("could not find metadata")

pypi_name = re.compile("^[a-z0-9]+(-[a-z0-9]+)*")

def parse_depend(depend):
    env = ""

    if ";" in depend:
        depend, _, marker_expr = depend.partition(";")
        marker_expr = marker_expr.strip()

        env = marker_expr

    depend = depend.lower().replace("_", "-")

    name = pypi_name.find(depend).strip()
    version = depend.removeprefix(name).strip()

    return name, version, env

def evaluate_marker(ctx, base, marker):
    if marker == "":
        return True

    abi = json.decode(ctx.build(get_python_abi(base)).read())

    return eval_starlark(marker, {
        "python_version": abi["version"],
        "sys_platform": abi["platform"],
        "extra": "",
        "platform_python_implementation": "",
    })

def _extract_wheel(ctx, base, name, version, filename, contents):
    metadata = json.decode(ctx.build(define.build(
        _get_metadata,
        name,
        version,
        define.read_archive(contents, ".zip"),
    )).read())

    ret = filesystem()

    ret[filename] = contents

    load_order = [filename]

    for depend in (metadata["Requires-Dist"] if "Requires-Dist" in metadata else []):
        depend_name, depend_version, marker = parse_depend(depend)

        if not evaluate_marker(ctx, base, marker):
            continue

        child = ctx.build(get_wheel(base, depend_name, depend_version)).read_archive()

        child_load_order = json.decode(child["load_order.json"].read())

        for file in reversed(child_load_order):
            if file in load_order:
                continue

            ret[file] = child[file]
            load_order = [file] + load_order

    ret["load_order.json"] = json.encode(load_order)

    return ctx.archive(ret)

def _get_sdist_build_fs(ctx, base, name, version, contents):
    metadata = json.decode(ctx.build(define.build(
        _get_metadata,
        name,
        version,
        define.read_archive(contents, ".tar.gz"),
    )).read())

    in_ark = filesystem(ctx.build(define.read_archive(contents, ".tar.gz")))

    ret = filesystem()

    ret["source"] = in_ark

    for depend in (metadata["Requires-Dist"] if "Requires-Dist" in metadata else []):
        return error("depends not implemented")

    return ctx.archive(ret)

additional_packages = {
    "Pillow": [
        query("zlib-dev"),
        query("jpeg-dev"),
    ],
}

def _build_sdist(ctx, base, name, version, url):
    build_fs = define.build(
        _get_sdist_build_fs,
        base,
        name,
        version,
        define.fetch_http(url),
    )

    vm_def = define.build_vm(
        [
            base.add_packages(
                [
                    query("build-base"),
                    query("python3-dev"),
                ] + (
                    additional_packages[name] if name in additional_packages else []
                ),
            ),
            build_fs,
            directive.run_command("set -e;mkdir /out;cd /source/{}-{};pip wheel -w /out .;tar cf /out.tar /out/{}-{}-*.whl".format(name, version, name, version)),
        ],
        output = "out.tar",
    )

    out = filesystem(ctx.build(define.read_archive(vm_def, ".tar")))["out"]

    whl = [f for f in out][0]

    return define.build(_extract_wheel, base, name, version, whl.base, whl)

def _get_wheel(ctx, base, name, version):
    # Download the metadata for pypi
    metadata = json.decode(ctx.build(define.fetch_http(
        "mirror://pypi/{}/".format(name),
        headers = {
            "Accept": "application/vnd.pypi.simple.v1+json",
        },
    )).read())

    # Get a list of working versions that match the request.
    versions = [
        ver
        for ver in sorted(metadata["versions"], reverse = True)
        if satisfies_version(ver, version)
    ]

    if len(versions) == 0:
        return error("could not find version for {}@{}".format(name, version))

    # The target version is the first version.
    target_version = versions[0]

    # Get the python abi tag list by running a child VM.
    abi = json.decode(ctx.build(get_python_abi(base)).read())

    abi_tags = abi["tags"]

    options = []

    # Check though all the files to find compatible sdists and wheels.
    for file in metadata["files"]:
        f = parse_filename(file["filename"])

        if "egg" in f:  # ignore eggs
            continue

        if "windows" in f:  # ignore windows
            continue

        if f["version"] != target_version:
            continue

        if not f["wheel"]:
            options.append((f, file))
        elif satisfies_wheel(f, abi_tags):
            options.append((f, file))

    # If there is a wheel then use it.
    for f, file in options:
        if f["wheel"]:
            return define.build(_extract_wheel, base, f["name"], f["version"], file["filename"], define.fetch_http(file["url"]))

    # Otherwise look for a source distribution and build it.
    # TODO(joshua): Implement this.

    if len(options) != 1:
        return error("more than one sdist option or no options found for {}@{}".format(name, version))

    f, file = options[0]

    return define.build(_build_sdist, base, f["name"], f["version"], file["url"])

def get_wheel(base, name, version):
    return define.build(_get_wheel, base, name, version)

def main(args):
    base = define.plan(
        builder = "alpine@3.20",
        packages = [
            query("py3-pip"),
        ],
        tags = ["level3", "defaults"],
    )

    top = db.build(get_wheel(base, "matplotlib", ""))

    fs = top.read_archive()

    print([f for f in fs])
    print(json.decode(fs["load_order.json"].read()))

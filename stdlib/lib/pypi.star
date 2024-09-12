db.add_mirror("pypi", ["https://pypi.org/simple"])

def parse_filename(filename):
    if filename.endswith(".whl"):
        filename = filename.removesuffix(".whl")

        name, _, rest = filename.partition("-")
        version, _, abi = rest.partition("-")

        abi = abi.split("-")

        if len(abi) > 3:
            abi = abi[len(abi) - 3:]

        return {
            "wheel": True,
            "name": name,
            "version": version,
            "abi": abi,
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
        return {"native": True}
    elif filename.endswith(".msi"):
        return {"native": True}
    elif filename.endswith(".dmg"):
        return {"native": True}
    elif filename.endswith(".rpm"):
        return {"native": True}
    else:
        return error("{} not implemented".format(filename))

alpha_version = re.compile(".*a[0-9]+")
rc_version = re.compile(".*rc[0-9]+")

def satisfies_version(pkg_version, target_version):
    target_version = target_version.strip()

    if target_version == "":
        return True

    if target_version.startswith("(") and target_version.endswith(")"):
        target_version = target_version[1:-1]

    if "," in target_version:
        versions = target_version.split(",")

        for ver in versions:
            if not satisfies_version(pkg_version, ver):
                return False

        return True

    is_alpha = alpha_version.find(pkg_version) != None
    is_rc = rc_version.find(pkg_version) != None

    if is_alpha or is_rc:
        return False

    if target_version.startswith(">="):
        target_version = target_version.removeprefix(">=")
        return pkg_version >= target_version
    elif target_version.startswith(">"):
        target_version = target_version.removeprefix(">")
        return pkg_version > target_version
    elif target_version.startswith("<"):
        target_version = target_version.removeprefix("<")
        return pkg_version < target_version
    elif target_version.startswith("!="):
        target_version = target_version.removeprefix("!=")
        return pkg_version != target_version
    elif target_version.startswith("=="):
        target_version = target_version.removeprefix("==")
        if target_version.endswith("*"):
            target_version = target_version.removesuffix("*")
            return pkg_version.startswith(target_version)
        else:
            return pkg_version == target_version
    elif target_version.startswith("~="):
        target_version = target_version.removeprefix("~=")
        return pkg_version.startswith(target_version)
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
import platform

lst = [[tag.interpreter, tag.abi, tag.platform] for tag in packaging.tags.sys_tags()]

ret = {
  "tags": {},
  "version": ".".join([
    str(sys.version_info.major),
    str(sys.version_info.minor),
    str(sys.version_info.micro),
  ]),
  "platform": sys.platform,
  "platform_system": platform.system(),
  "platform_python_implementation": platform.python_implementation(),
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

pypi_name = re.compile("^[a-z0-9\\.]+(-[a-z0-9\\.]+)*")

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
        "platform_python_implementation": abi["platform_python_implementation"],
        "platform_system": abi["platform_system"],
    })

def _extract_wheel(ctx, base, name, version, include_deps, filename, contents):
    tokens = filename.split("-")

    name = tokens[0]
    version = tokens[1]

    if not include_deps:
        ret = filesystem()

        ret[filename] = contents

        ret["load_order.json"] = json.encode([filename])

        return ctx.archive(ret)

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
    in_ark = filesystem(ctx.build(define.read_archive(contents, ".tar.gz")))

    ret = filesystem()

    ret["source"] = in_ark

    return ctx.archive(ret)

additional_packages = {
    "Pillow": [
        query("zlib-dev"),
        query("jpeg-dev"),
    ],
    "scipy": [
        query("gfortran"),
        query("openblas-dev"),
    ],
}

download_overrides = {
    # override this since the version published on pypi has missing header files.
    "pyradiomics@3.1.0": "https://github.com/AIM-Harvard/pyradiomics/archive/refs/tags/v3.1.0.tar.gz",
    "surfa@0d83332351083b33c4da221e9d10a63a93ae7f52": "https://github.com/freesurfer/surfa/archive/7ca713d2b0c2c9e4f3471cd14ee5e12a00d3b631.tar.gz",
}

def version_compare(a, b):
    if a == b:
        return 0

    a_tokens = a.split(".")
    b_tokens = b.split(".")

    for a, b in zip(a_tokens, b_tokens):
        if "rc" in a or "rc" in b:
            a1, _, a2 = a.partition("rc")
            b1, _, b2 = b.partition("rc")

            a_int = parse_int(a1)
            b_int = parse_int(b1)
            if a_int < b_int:
                return 1
            elif a_int > b_int:
                if a2 != "":
                    if b2 != "":
                        a2_int = parse_int(a2)
                        b2_int = parse_int(b2)
                        if a2_int > b2_int:
                            return -1
                        elif a2_int < b2_int:
                            return 1
                    else:
                        return 1

                return -1
        else:
            a_int = parse_int(a)
            b_int = parse_int(b)
            if a_int < b_int:
                return 1
            elif a_int > b_int:
                return -1

    return 0

build_packages = [
    query("build-essential"),
    query("python3-dev"),
]

def _build_sdist(ctx, base, name, version, include_deps, url):
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
                build_packages + (
                    additional_packages[name] if name in additional_packages else []
                ),
            ),
            build_fs,
            directive.run_command("\n".join([
                "set -e",
                "mkdir /out",
                "cd /source/{}-{}".format(name, version),
                "pip3 wheel -w /out .",
                "tar cf /out.tar /out/{}-*.whl".format(name),
            ])),
        ],
        output = "out.tar",
        cpu_cores = 2,
        memory_mb = 4 * 1024,
        storage_size = 4 * 1024,
    )

    out = filesystem(ctx.build(define.read_archive(vm_def, ".tar")))["out"]

    whl = [f for f in out][0]

    return define.build(
        _extract_wheel,
        base,
        name,
        version,
        include_deps,
        whl.base,
        whl,
    )

def _get_wheel(ctx, base, name, version, include_deps):
    print("get_wheel", name, version)

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

    versions = sort(versions, version_compare)

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

        if "native" in f:  # ignore native installers
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
            return define.build(
                _extract_wheel,
                base,
                f["name"],
                f["version"],
                include_deps,
                file["filename"],
                define.fetch_http(file["url"]),
            )

    # Otherwise look for a source distribution and build it.

    if len(options) != 1:
        return error("more than one sdist option or no options found for {}@{}".format(name, version))

    f, file = options[0]

    override_key = "{}@{}".format(f["name"], f["version"])

    if override_key in download_overrides:
        return define.build(
            _build_sdist,
            base,
            f["name"],
            f["version"],
            include_deps,
            download_overrides[override_key],
        )
    else:
        return define.build(
            _build_sdist,
            base,
            f["name"],
            f["version"],
            include_deps,
            file["url"],
        )

def get_wheel(base, name, version, include_deps = True):
    return define.build(_get_wheel, base, name, version, include_deps)

def _get_wheel_from_url(ctx, base, name, url, include_deps):
    if not url.startswith("git+"):
        return error("unsupported url: " + url)

    url = url.removeprefix("git+")

    if not url.startswith("https://github.com/"):
        return error("unsupported url: " + url)

    repo, rev = url.split("@")

    if not repo.endswith(".git"):
        return error("unsupported url: " + repo)

    repo = repo.removesuffix(".git")

    url = "{}/archive/{}.tar.gz".format(repo, rev)

    override_key = "{}@{}".format(name, rev)

    if override_key in download_overrides:
        url = download_overrides[override_key]
        rev = url.rpartition("/")[2].removesuffix(".tar.gz")
        return define.build(
            _build_sdist,
            base,
            name,
            rev,
            include_deps,
            url,
        )
    else:
        return define.build(
            _build_sdist,
            base,
            name,
            rev,
            include_deps,
            url,
        )

def get_wheel_from_url(base, name, url, include_deps = True):
    return define.build(_get_wheel_from_url, base, name, url, include_deps)

def _build_run_fs(ctx, wheel):
    wheel = filesystem(wheel.read_archive())

    ret = filesystem()

    ret["wheels"] = wheel

    scripts = []

    load_order = json.decode(wheel["load_order.json"].read())

    for filename in load_order:
        scripts.append({
            "kind": "execute",
            "exec": "/usr/bin/pip",
            "args": ["install", "--no-deps", "/wheels/" + filename],
        })

    ret["wheels/scripts.json"] = file(json.encode(scripts))

    return ctx.archive(ret)

def build_run_fs(wheel):
    return define.build(_build_run_fs, wheel)

def _build_fs_for_requirements(ctx, base, requirements):
    ret = filesystem()

    requirement_list = requirements.read().splitlines()

    scripts = []

    for requirement in requirement_list:
        wheel = None
        if " @ " in requirement:
            name, _, url = requirement.partition(" @ ")

            wheel = ctx.build(get_wheel_from_url(base, name, url, include_deps = False)).read_archive()
        else:
            name, version, _ = parse_depend(requirement)

            wheel = ctx.build(get_wheel(base, name, version, include_deps = False)).read_archive()

        load_order = json.decode(wheel["load_order.json"].read())

        for filename in load_order:
            ret["wheels/" + filename] = wheel[filename]

            scripts.append({
                "kind": "execute",
                "exec": "/usr/bin/pip",
                "args": ["install", "--no-deps", "/wheels/" + filename],
            })

    ret["wheels/scripts.json"] = file(json.encode(scripts))

    return ctx.archive(ret)

def build_fs_for_requirements(base, requirements):
    return define.build(_build_fs_for_requirements, base, requirements)

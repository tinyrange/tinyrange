def read_octave_line(line):
    if not line.startswith("# "):
        return error("invalid octave entry: " + line)

    line = line.removeprefix("# ")

    k, _, v = line.partition(": ")

    return k, v

def check_octave_name(line, expected):
    name, value = read_octave_line(line)

    if name != expected:
        return error("expected {} got {}".format(expected, name))

    return value

def octave_validate_traller(rest):
    if rest[0] != "" or rest[1] != "":
        print(rest)
        return error("invalid traller")

    return rest[2:]

def parse_octave_entry(lines):
    name = check_octave_name(lines[0], "name")
    typ = check_octave_name(lines[1], "type")

    if typ == "cell":
        rows = int(check_octave_name(lines[2], "rows"))
        columns = int(check_octave_name(lines[3], "columns"))

        if rows != 1:
            return error("rows != 1")

        ret = []

        rest = lines[4:]

        for _ in range(columns):
            _, val, rest = parse_octave_entry(rest)
            ret.append(val)

        rest = octave_validate_traller(rest)

        return name, ret, rest
    elif typ == "scalar struct":
        ndims = int(check_octave_name(lines[2], "ndims"))
        length = int(check_octave_name(lines[4], "length"))

        rest = lines[5:]

        ret = {}

        for _ in range(length):
            child_name, child, rest = parse_octave_entry(rest)

            ret[child_name] = child

        rest = octave_validate_traller(rest)

        rest = rest[1:]

        return name, ret, rest
    elif typ == "sq_string":
        elements = int(check_octave_name(lines[2], "elements"))
        length = int(check_octave_name(lines[3], "length"))

        if elements != 1:
            return error("elements != 1")

        value = lines[4]

        lines = lines[5:]

        lines = octave_validate_traller(lines)

        return name, value, lines
    elif typ == "bool":
        if lines[2] == "0":
            lines = lines[3:]
            lines = octave_validate_traller(lines)
            return name, False, lines
        elif lines[2] == "1":
            lines = lines[3:]
            lines = octave_validate_traller(lines)
            return name, True, lines
        else:
            return error("bool not implemented")
    else:
        return error("type {} not implemented".format(typ))

def parse_octave_file(contents):
    lines = contents.splitlines()

    lines = lines[1:]

    _, ent, _ = parse_octave_entry(lines)

    return ent

def parse_compiled_octave_package(ctx, ark, name, _):
    fs = filesystem(ark)

    contents = fs["usr/share/octave/octave_packages"].read()

    parsed_package_file = parse_octave_file(contents)

    if len(parsed_package_file) > 1:
        parsed_package_file = [ent for ent in parsed_package_file if ent["dir"] == "__OH__/share/octave/packages/" + name]

    if len(parsed_package_file) == 0:
        return error("could not find package in file")

    out = filesystem()

    out[".pkg/octave-metadata/{}.json".format(name)] = json.encode(parsed_package_file)
    out["usr/share/octave/packages/" + name] = fs["usr/share/octave/packages/" + name]
    out["usr/lib/octave/packages/" + name] = fs["usr/lib/octave/packages/" + name]

    return ctx.archive(out)

def write_octave_line(name, value):
    return "# {}: {}".format(name, value)

def write_octave_entry(ent, name):
    ret = []
    ret.append(write_octave_line("name", name))

    if type(ent) == "list":
        ret.append(write_octave_line("type", "cell"))
        ret.append(write_octave_line("rows", "1"))
        ret.append(write_octave_line("columns", len(ent)))

        for child in ent:
            ret += write_octave_entry(child, "<cell-element>")

        ret.append("")
        ret.append("")

        return ret
    elif type(ent) == "dict":
        ret.append(write_octave_line("type", "scalar struct"))
        ret.append(write_octave_line("ndims", "2"))
        ret.append(" 1 1")
        ret.append(write_octave_line("length", str(len(ent))))

        for k in ent:
            ret += write_octave_entry(ent[k], k)

        ret.append("")
        ret.append("")

        return ret
    elif type(ent) == "string":
        ret.append(write_octave_line("type", "sq_string"))
        ret.append(write_octave_line("elements", "1"))
        ret.append(write_octave_line("length", str(len(ent))))
        ret.append(ent)

        ret.append("")
        ret.append("")

        return ret
    elif type(ent) == "bool":
        ret.append(write_octave_line("type", "bool"))
        if ent:
            ret.append("1")
        else:
            ret.append("0")

        ret.append("")
        ret.append("")

        return ret
    else:
        return error("type not implemented: {}".format(type(ent)))

def write_octave_file(ent):
    lines = ["# Created by Octave 9.1.0"]

    lines += write_octave_entry(ent, "global_packages")

    return "\n".join(lines)

def build_octave_package_file(ctx, packages):
    metadata = []

    for pkg in packages:
        pkg_fs = filesystem(pkg.read_archive())

        pkg_metadata = json.decode([f for f in pkg_fs[".pkg/octave-metadata"]][0].read())

        metadata += pkg_metadata

    fs = filesystem()

    fs["usr/share/octave/octave_packages"] = write_octave_file(metadata)

    return ctx.archive(fs)

def add_octave_packages(packages):
    if len(packages) == 0:
        return []

    return [directive.archive(pkg) for pkg in packages] + [define.build(
        build_octave_package_file,
        packages,
    )]

def build_octave_package(url, name, additional_queries = [], depends = []):
    vm = define.build_vm(
        directives = [
            define.plan(
                builder = "alpine@3.20",
                packages = [
                    query("octave"),
                    query("octave-dev"),
                    query("build-base"),
                    query("texinfo"),
                ] + additional_queries,
                tags = ["level3", "defaults"],
            ),
        ] + add_octave_packages(depends) + [
            # Ask Octave to install the package.
            directive.run_command("octave --eval 'pkg install \"{}\"'".format(url)),
            # Compress the package into a tar.gz file which is sent back to the host.
            directive.run_command("tar czf /package.tar.gz " +
                                  "/usr/lib/octave/packages/{} ".format(name) +
                                  "/usr/share/octave/packages/{} ".format(name) +
                                  "/usr/share/octave/octave_packages"),
        ],
        output = "/package.tar.gz",
    )

    return define.build(
        parse_compiled_octave_package,
        define.read_archive(vm, ".tar.gz"),
        name,
        "v2",
    )

octave_forge = "https://downloads.sourceforge.net/project/octave/Octave%20Forge%20Packages/Individual%20Package%20Releases/"

pkg_image = build_octave_package(
    octave_forge + "image-2.14.0.tar.gz",
    "image-2.14.0",
)

pkg_io = build_octave_package(
    octave_forge + "io-2.6.0.tar.gz",
    "io-2.6.0",
)

pkg_struct = build_octave_package(
    octave_forge + "struct-1.0.18.tar.gz",
    "struct-1.0.18",
)

pkg_statistics = build_octave_package(
    "https://github.com/gnu-octave/statistics/archive/refs/tags/release-1.6.7.tar.gz",
    "statistics-1.6.7",
)

pkg_optim = build_octave_package(
    octave_forge + "optim-1.6.2.tar.gz",
    "optim-1.6.2",
    depends = [pkg_struct, pkg_statistics],
    additional_queries = [query("openblas-dev")],
)

vm_test = define.build_vm(
    directives = [
        define.plan(
            builder = "alpine@3.20",
            packages = [
                query("octave"),
                query("texinfo"),
            ],
            tags = ["level3", "defaults"],
        ),
    ] + add_octave_packages([
        pkg_image,
        pkg_io,
        pkg_struct,
        pkg_statistics,
        pkg_optim,
    ]) + [
        directive.run_command("interactive"),
    ],
)

qmr_lab_test = define.build_vm(
    directives = [
        define.plan(
            builder = "alpine@3.20",
            packages = [
                query("octave"),
                query("octave-dev"),
                query("build-base"),
                query("texinfo"),
            ],
            tags = ["level3", "defaults"],
        ),
    ] + add_octave_packages([
        pkg_image,
        pkg_io,
        pkg_struct,
        pkg_statistics,
        pkg_optim,
    ]) + [
        define.read_archive(
            define.fetch_http("https://github.com/qMRLab/qMRLab/archive/refs/heads/master.tar.gz"),
            ".tar.gz",
        ),
        directive.run_command("interactive"),
    ],
)

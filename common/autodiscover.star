def discover_go_builder(archive_name, archive):
    if "go.mod" not in archive:
        return None, False

    output_name = archive_name.name.replace("/", "_")

    b = builder(archive_name)

    path = b.add_archive(archive, filename = output_name)

    # TODO(joshua): Detect which version of go is needed by parsing go.mod.
    # TODO(joshua): Add dependancies for all the other modules so we can skip the download step eventually.
    b.add_dependency(name(name = "go"))

    b.add_script("go build -o {} .".format(output_name), cwd = path)

    b.add_output(output_name, cwd = path)

    return b, True

def discover_cmake_builder(archive_name, archive):
    if "CMakeLists.txt" not in archive:
        return None, False

    output_name = archive_name.name.replace("/", "_")

    b = builder(archive_name)

    path = b.add_archive(archive, filename = output_name)

    b.add_dependency(name(name = "cmake"))
    b.add_dependency(name(name = "gcc"))
    b.add_dependency(name(name = "make"))

    b.add_script("mkdir /build")
    b.add_script("cmake {}".format(path), cwd = "/build")
    b.add_script("make", cwd = "/build")

    return b, True

discovery_methods = [
    discover_go_builder,
    discover_cmake_builder,
]

def autodiscover_builder(name, archive):
    for method in discovery_methods:
        ret, ok = method(name, archive)
        if not ok:
            continue
        return ret, True

    return None, False

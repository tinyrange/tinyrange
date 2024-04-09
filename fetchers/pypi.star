parallel_jobs = 10

def parse_python_metadata(contents):
    lines = contents.splitlines()

    ret = {}
    last_key = ""

    for line in lines:
        line = line.strip("\r\n")

        # print("[", line, "]")

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

def parse_pypi_name(ctx, name):
    options = name.split(" | ")
    if len(options) > 1:
        return [parse_pypi_name(ctx, option) for option in options]
    else:
        version = ""

        if " (" in name:
            name, version = name.split(" (", 1)
            version = version.removesuffix(")")

        return ctx.name(name = name, version = version)

def add_pypi_package(ctx, download_url, metadata):
    # print(metadata)

    if "Name" not in metadata:
        return

    pkg = ctx.add_package(ctx.name(
        name = metadata["Name"][0],
        version = metadata["Version"][0],
    ))

    if "Summary" in metadata:
        pkg.set_description(metadata["Summary"][0])

    if "License" in metadata:
        pkg.set_license(metadata["License"][0])

    pkg.add_source(url = download_url)

    if "Requires-Dist" in metadata:
        for depend in metadata["Requires-Dist"]:
            pkg.add_dependency(parse_pypi_name(ctx, depend))

def fetch_pypi_package_versions(ctx, url, proj):
    project_url = "{}/simple/{}/".format(url, proj["name"])

    resp = fetch_http(
        project_url,
        accept = "application/vnd.pypi.simple.v1+json",
        use_etag = True,
        fast = True,  # Allow parallel downloads since we're hitting a well provisioned CDN.
    )
    if resp == None:
        print("warn: could not get project: ", proj["name"])
        return

    proj_resp = json.decode(resp.read())

    for file in reversed(proj_resp["files"]):
        download_url = file["url"]

        if download_url.endswith(".whl"):
            if file["core-metadata"] == False:
                print("warn: no metadata for wheel: ", download_url)
                continue

            metadata_url = download_url + ".metadata"

            metadata_contents = fetch_http(metadata_url, fast = True)
            if metadata_contents == None:
                print("warn: could not fetch metadata: ", metadata_url)
                continue

            add_pypi_package(ctx, download_url, parse_python_metadata(metadata_contents.read()))

            return
        elif download_url.endswith(".tar.gz") or download_url.endswith(".tar.bz2") or download_url.endswith(".tgz") or download_url.endswith(".zip"):
            # Assume source

            pass
        elif download_url.endswith(".egg") or download_url.endswith(".exe"):
            # Not Handled.

            pass
        else:
            # print("unknown download url: ", download_url)

            pass

def fetch_pypi_repository(ctx, url):
    simple_url = url + "/simple/"

    resp = json.decode(fetch_http(
        simple_url,
        accept = "application/vnd.pypi.simple.v1+json",
        use_etag = True,
    ).read())

    ctx.parallel_for(resp["projects"], fetch_pypi_package_versions, (url,), jobs = parallel_jobs)

if __name__ == "__main__":
    fetch_repo(fetch_pypi_repository, ("https://pypi.org",), distro = "python")

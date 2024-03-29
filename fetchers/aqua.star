"""
Package Fetcher for Aqua https://github.com/aquaproj/aqua-registry
"""

def handle_aqua_package(ctx, tree):
    name = tree.name.removeprefix("pkgs/")
    pkg = parse_yaml(tree["pkg.yaml"].read())
    registry = parse_yaml(tree["registry.yaml"].read())

    for ver in pkg["packages"]:
        name = ver["name"]
        version = ""
        if "version" in ver:
            version = ver["version"]
        else:
            name, version = name.split("@")

        ctx.add_package(ctx.name(
            name = name,
            version = version,
        ))

def fetch_aqua_registry(ctx, url):
    repo = fetch_git(url)

    main = repo.branch("main")

    for owner in main["pkgs"]:
        if type(owner) != "GitTree":
            continue
        for package in owner:
            if "pkg.yaml" in package:
                handle_aqua_package(ctx, package)
            else:
                for child in package:
                    handle_aqua_package(ctx, child)

fetch_repo(
    fetch_aqua_registry,
    ("https://github.com/aquaproj/aqua-registry.git",),
    distro = "aqua",
)

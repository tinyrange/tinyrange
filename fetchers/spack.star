"""
Package metadata fetcher for Spack
"""

def fetch_spack_repository(ctx):
    repo = fetch_git("https://github.com/spack/packages.spack.io")
    main_branch = repo.branch("main")

    for pkg_file in main_branch["data/packages"]:
        data = json.decode(pkg_file.read())

        for version in data["versions"]:
            pkg = ctx.add_package(ctx.name(
                name = data["name"],
                version = version["name"],
            ))
            pkg.set_description(data["description"])

            for depend in data["dependencies"]:
                pkg.add_dependency(ctx.name(
                    name = depend["name"],
                ))

if __name__ == "__main__":
    fetch_repo(fetch_spack_repository, (), distro = "spack")

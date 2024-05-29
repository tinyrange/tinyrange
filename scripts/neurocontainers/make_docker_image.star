load("fetchers/neurocontainers.star", "fetch_neurocontainers_repository")
load("repos/rpm.star", "add_fedora_fetchers")

def main(ctx):
    result = ctx.query(name(ctx.args[0], distribution = "neurocontainers"))
    if len(result) == 0:
        print("package not found")
        return

    pkg = result[0]

    builder = json.decode(pkg.builders[0].json_string)

    depends = []

    for directive in builder["Directives"]:
        # TODO(joshua): Handle other directives.
        if directive["Kind"] == "Dependency":
            depends.append(name(name = directive["Name"]["Name"], distribution = "fedora@35", architecture = "x86_64"))

    plan = ctx.plan(prefer_architecture = "x86_64", *depends)

    print(plan)

if __name__ == "__main__":
    fetch_repo(
        fetch_neurocontainers_repository,
        ("https://github.com/NeuroDesk/neurocontainers", "master"),
        distro = "neurocontainers",
    )
    add_fedora_fetchers(only_version = "35")

    run_script(main)

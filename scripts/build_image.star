plan = define.plan(
    builder = "alpine@3.20",
    packages = [query("build-base")],
    tags = ["level3", "defaults"],
)

def package_init(ctx, arch):
    fs = filesystem()

    fs["/init"] = file(db.get_builtin_executable("init", arch), executable = True)

    return ctx.archive(fs, kind = ".tar.gz")

def write_filesystem_from_plan(ctx, plan):
    fs, commands = plan.filesystem()

    print(commands)

    return ctx.archive(fs, kind = ".tar.gz")

def build_docker_image(ctx, fragments):
    layers = []

    layers.append(ctx.build(define.build(package_init, "x86_64")))

    for frag in fragments:
        if type(frag) == "PlanDefinition":
            build_def = define.build(write_filesystem_from_plan, frag)
            layers.append(ctx.build(build_def))
        else:
            return error("{} not implemented".format(type(frag)))

    print(layers)

    return error("not implemented")

docker = define.build(build_docker_image, [plan])

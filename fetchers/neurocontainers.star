load("common/shell.star", "register_commands")
load("fetchers/neurodocker.star", "get_neurodocker_package")

def parse_neurodocker_package(ret, args, name):
    if "pkgs" not in ret:
        ret["pkgs"] = {}
    ret["pkgs"][name] = {}

    index = 1
    for arg in args[1:]:
        if arg.startswith("--"):
            break

        key, _, value = arg.partition("=")
        ret["pkgs"][name][key] = value
        index += 1

    return parse_neurodocker_args(ret, args[index:])

packages = [
    "convert3d",
    "minc",
    "mrtrix3",
    "mrtrix3tissue",
    "afni",
    "fsl",
    "miniconda",
    "ants",
    "ashs",
    "dcm2niix",
    "matlabmcr",
    "cat12",
    "freesurfer",
]

def parse_neurodocker_args(ret, args):
    if len(args) == 0:
        return ret

    if args[0] == "--base-image":
        ret["base-image"] = args[1]

        return parse_neurodocker_args(ret, args[2:])
    elif args[0] == "--pkg-manager":
        ret["pkg-manager"] = args[1]

        return parse_neurodocker_args(ret, args[2:])
    elif args[0] == "--entrypoint":
        ret["entrypoint"] = args[1]

        return parse_neurodocker_args(ret, args[2:])
    elif args[0] == "--workdir":
        ret["workdir"] = args[1]

        return parse_neurodocker_args(ret, args[2:])
    elif args[0] == "--user":
        ret["user"] = args[1]

        return parse_neurodocker_args(ret, args[2:])
    elif args[0] == "--env":
        if "env" not in ret:
            ret["env"] = []

        index = 1
        for arg in args[1:]:
            if arg.startswith("--"):
                break

            ret["env"].append(arg)
            index += 1

        return parse_neurodocker_args(ret, args[index:])
    elif args[0] == "--copy":
        if "copy" not in ret:
            ret["copy"] = []

        ret["copy"].append((args[1], args[2]))

        return parse_neurodocker_args(ret, args[3:])
    elif args[0] == "--add":
        if "add" not in ret:
            ret["add"] = []

        ret["add"].append((args[1], args[2]))

        return parse_neurodocker_args(ret, args[3:])
    elif args[0] == "--copy-from":
        if "copy" not in ret:
            ret["copy"] = []

        ret["copy"].append((args[1] + ":" + args[2], args[3]))

        return parse_neurodocker_args(ret, args[4:])
    elif args[0] == "--install":
        if "install" not in ret:
            ret["install"] = []

        index = 1
        for arg in args[1:]:
            if arg.startswith("--"):
                break

            if arg.startswith("opts="):
                index += 1
                continue

            ret["install"].append(arg)
            index += 1

        return parse_neurodocker_args(ret, args[index:])
    elif args[0] == "--run":
        if "run" not in ret:
            ret["run"] = []

        ret["run"].append(args[1])

        return parse_neurodocker_args(ret, args[2:])
    elif args[0].startswith("--run="):
        if "run" not in ret:
            ret["run"] = []

        ret["run"].append(args[0].removeprefix("--run="))

        return parse_neurodocker_args(ret, args[1:])
    elif args[0].startswith("--run-bash="):
        if "run" not in ret:
            ret["run"] = []

        ret["run"].append(args[0].removeprefix("--run-bash="))

        return parse_neurodocker_args(ret, args[1:])
    elif args[0].startswith("--workdir="):
        ret["workdir"] = args[0].removeprefix("--workdir=")

        return parse_neurodocker_args(ret, args[1:])
    elif args[0].startswith("--user="):
        ret["user"] = args[0].removeprefix("--user=")

        return parse_neurodocker_args(ret, args[1:])
    elif args[0].startswith("--install="):
        if "install" not in ret:
            ret["install"] = []

        s = args[0].removeprefix("--install=")

        for pkg in s.split(" "):
            ret["install"].append(s)

        return parse_neurodocker_args(ret, args[1:])
    else:
        for pkg in packages:
            if args[0] == "--" + pkg:
                return parse_neurodocker_package(ret, args, pkg)

        return error("argument not implemented: " + args[0])

def cmd_neurodocker(ctx, args):
    cmd = args[1]
    if cmd != "generate":
        return error("unknown neurodocker command: " + cmd)

    mode = args[2]
    if mode != "docker":
        return error("unknown neurodocker mode: " + mode)

    rest = args[3:]

    obj = parse_neurodocker_args({}, rest)

    ctx["neurodocker"] = obj

    return json.encode(obj)

def cmd_pip(ctx, args):
    args = args[1:]

    if args[0] == "uninstall":
        return ""

    if args[0] != "install":
        return error("unknown pip command: " + json.encode(args))

    args = args[1:]

    args = [k for k in args if not k.startswith("--")]

    url = args[0]

    ctx.set_environment("neurodocker_url", url)

    return ""

def cmd_tinyrange(ctx, args):
    print("tinyrange", args)
    return ""

def eval_neurocontainer_build(contents):
    ctx = shell_context()

    ctx.set_environment("neurodocker_buildMode", "docker")
    ctx.set_environment("neurodocker_buildExt", ".Dockerfile")
    ctx.set_environment("mountPointList", "")
    ctx.set_environment("TINYRANGE", "tinyrange")
    ctx.set_environment("neurodocker_url", "https://github.com/ReproNim/neurodocker@master")

    register_commands(ctx)

    ctx.add_command("neurodocker", cmd_neurodocker)
    ctx.add_command("pip", cmd_pip)
    ctx.add_command("tinyrange", cmd_tinyrange)

    ret = ctx.eval(contents)

    return ret

def make_builder_from_neurodocker_recipe(pkg_name, recipe, neurodocker_url):
    build = builder(pkg_name)

    build.set_base_image(recipe["base-image"])

    pkg_manager = recipe["pkg-manager"]

    if "install" in recipe:
        if "pkg-manager" not in recipe:
            return error("no pkg-system specified for install")
        for install in recipe["install"]:
            build.add_dependency(name(name = install))

    if "run" in recipe:
        for script in recipe["run"]:
            build.add_script(script)

    url = neurodocker_url

    # All these URLs point to old branches which are now broken.
    if url == "https://github.com/NeuroDesk/neurodocker/tarball/update_cat" or \
       url == "https://github.com/NeuroDesk/neurodocker/tarball/update_mcr" or \
       url == "https://github.com/NeuroDesk/neurodocker/tarball/minc_install_from_deb_and_rpm":
        url = "https://github.com/ReproNim/neurodocker@master"

    branch = ""
    if "/tarball/" in url:
        url, branch = url.split("/tarball/")
    else:
        url, branch = url.split("@")
        if url.startswith("git+https"):
            url = url.removeprefix("git+")

    if "pkgs" in recipe:
        for pkg in recipe["pkgs"]:
            ret = get_neurodocker_package(
                url,
                branch,
                pkg,
                pkg_manager,
                recipe["pkgs"][pkg],
            )

            for install in ret["packages"]:
                build.add_dependency(name(name = install))

            for env in ret["environment"]:
                # TODO(joshua): Add support for adding environment variables.
                pass

            build.add_script(ret["script"])
            # print("install", pkg, ret)

    return build

def fetch_neurocontainers_repository(ctx, url, ref):
    repo = fetch_git(url)
    ref = repo.branch(ref)

    for folder in ref["recipes"]:
        if type(folder) != "GitTree":
            continue

        if "build.sh" not in folder:
            continue

        file = folder["build.sh"]
        if file.name == "recipes/mrtrix3tissue/build.sh" or \
           file.name == "recipes/samri/build.sh":
            continue

        # print("file", file)
        contents = file.read()

        ret = eval_neurocontainer_build(contents)

        name = ctx.name(
            name = ret["toolName"],
            version = ret["toolVersion"],
        )

        pkg = ctx.add_package(name)

        if "neurodocker" in ret:
            recipe = ret["neurodocker"]
            pkg.add_builder(make_builder_from_neurodocker_recipe(
                name,
                recipe,
                ret["neurodocker_url"],
            ))

if __name__ == "__main__":
    fetch_repo(fetch_neurocontainers_repository, ("https://github.com/NeuroDesk/neurocontainers", "master"), distro = "neurocontainers")

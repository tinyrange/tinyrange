GIT_REPO = define.read_archive(
    define.fetch_http("https://github.com/NeuroDesk/neurocontainers/archive/refs/heads/master.tar.gz"),
    ".tar.gz",
)

def cmd_wget(proc, args):
    if args[1] == "-O-":
        res = db.build(define.fetch_http(args[2]))
        proc.write(res.read())
        return None
    else:
        return error("wget not implemented: " + str(args))

def cmd_pip(proc, args):
    print("pip", args)

def cmd_python(proc, args):
    print("python", args)

def cmd_date(proc, args):
    proc.write("placeholder_date")

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

def parse_neurodocker_package(proc, ret, args, name):
    pkg_args = {"pkg": name, "args": {}}

    index = 1
    for arg in args[1:]:
        if arg.startswith("--"):
            break

        key, _, value = arg.partition("=")
        pkg_args["args"][key] = value
        index += 1

    ret.append(pkg_args)

    return parse_neurodocker_args(proc, ret, args[index:])

def parse_neurodocker_args(proc, ret, args):
    if len(args) == 0:
        return ret

    if args[0] == "--base-image":
        ret.append({"base-image": args[1]})

        return parse_neurodocker_args(proc, ret, args[2:])
    elif args[0] == "--pkg-manager":
        ret.append({"pkg-manager": args[1]})

        return parse_neurodocker_args(proc, ret, args[2:])
    elif args[0] == "--entrypoint":
        ret.append({"entrypoint": args[1]})

        return parse_neurodocker_args(proc, ret, args[2:])
    elif args[0] == "--workdir":
        ret.append({"workdir": args[1]})

        return parse_neurodocker_args(proc, ret, args[2:])
    elif args[0] == "--user":
        ret.append({"user": args[1]})

        return parse_neurodocker_args(proc, ret, args[2:])
    elif args[0] == "--env":
        index = 1
        obj = {}
        for arg in args[1:]:
            if arg.startswith("--"):
                break

            if arg != "":
                k, _, v = arg.partition("=")
                obj[k] = v

            index += 1

        ret.append({"env": obj})
        return parse_neurodocker_args(proc, ret, args[index:])
    elif args[0] == "--copy":
        contents = ""
        if "*" not in args[1]:
            contents = proc[args[1]].read()

        ret.append({"copy": args[2], "filename": args[1], "contents": contents})

        return parse_neurodocker_args(proc, ret, args[3:])
    elif args[0] == "--add":
        ret.append({"add": [args[1], args[2]]})

        return parse_neurodocker_args(proc, ret, args[3:])
    elif args[0] == "--copy-from":
        ret.append({"copy-from": [args[1], args[2], args[3]]})

        return parse_neurodocker_args(proc, ret, args[4:])
    elif args[0] == "--install":
        index = 1
        install_args = []
        for arg in args[1:]:
            if arg.startswith("--"):
                break

            if arg.startswith("opts="):
                index += 1
                continue

            if arg != "":
                install_args.append(arg)

            index += 1

        ret.append({"install": install_args})

        return parse_neurodocker_args(proc, ret, args[index:])
    elif args[0] == "--run":
        ret.append({"run": args[1]})

        return parse_neurodocker_args(proc, ret, args[2:])
    elif args[0].startswith("--run="):
        ret.append({"run": args[0].removeprefix("--run=")})

        return parse_neurodocker_args(proc, ret, args[1:])
    elif args[0].startswith("--run-bash="):
        ret.append({"run": args[0].removeprefix("--run-bash="), "bash": True})

        return parse_neurodocker_args(proc, ret, args[1:])
    elif args[0].startswith("--workdir="):
        ret.append({"workdir": args[0].removeprefix("--workdir=")})

        return parse_neurodocker_args(proc, ret, args[1:])
    elif args[0].startswith("--user="):
        ret.append({"user": args[0].removeprefix("--user=")})

        return parse_neurodocker_args(proc, ret, args[1:])
    elif args[0].startswith("--install="):
        s = args[0].removeprefix("--install=")

        install_args = []

        for pkg in s.split(" "):
            install_args.append(pkg)

        ret.append({"install": install_args})

        return parse_neurodocker_args(proc, ret, args[1:])
    elif args[0] == "":
        return parse_neurodocker_args(proc, ret, args[1:])
    else:
        for pkg in packages:
            if args[0] == "--" + pkg:
                return parse_neurodocker_package(proc, ret, args, pkg)

        return error("argument not implemented: " + args[0])

def optimise_args(args):
    ret = []
    last = None

    base_image = ""
    pkg_manager = ""

    for arg in args:
        if "base-image" in arg:
            base_image = arg["base-image"]
            continue
        elif "pkg-manager" in arg:
            pkg_manager = arg["pkg-manager"]
            continue

        if last == None:
            last = arg
            continue

        if "env" in last and "env" in arg:
            for k in arg["env"]:
                last["env"][k] = arg["env"][k]
        elif "install" in last and "install" in arg:
            last["install"] += arg["install"]
        else:
            ret.append(last)
            last = arg

    return ret + [last], base_image, pkg_manager

def cmd_neurodocker(proc, args):
    args = parse_neurodocker_args(proc, [], args[3:])

    args, base_image, pkg_manager = optimise_args(args)

    proc["/output.yml"] = json.encode({
        "name": proc.env("toolName"),
        "version": proc.env("toolVersion"),
        "base_image": base_image,
        "pkg_manager": pkg_manager,
        "args": args,
    })

def cmd_sed(proc, args):
    print("sed", args)

def create_neurocontainer_emulator(emu):
    emu.add_command("wget", cmd_wget)
    emu.add_command("pip", cmd_pip)
    emu.add_command("python", cmd_python)
    emu.add_command("date", cmd_date)
    emu.add_command("neurodocker", cmd_neurodocker)
    emu.add_command("sed", cmd_sed)

EXCLUDED_RECIPES = [
    "neurocontainers-master/recipes/afni",  # AFNI.version is empty (19/08/2024)
    "neurocontainers-master/recipes/brkraw",  # Uses old TinyRange to generate.
    "neurocontainers-master/recipes/cartool",  # Non-working with addtional commands for testing.
    "neurocontainers-master/recipes/itksnap",  # Has option for old TinyRange build.
]

def main(args):
    files = filesystem(db.build(GIT_REPO))["neurocontainers-master"]

    for recipe in files["recipes"]:
        if type(recipe) != "Directory":
            continue

        build_script = recipe["build.sh"].base
        build_script_dir = recipe["build.sh"].dir

        if build_script_dir in EXCLUDED_RECIPES:
            continue

        # print(build_script_dir)

        result = db.build(define.build_emulator(
            directives = [
                GIT_REPO,
                directive.run_command("cd {};neurodocker_buildMode=tinyrange /bin/bash {}".format(build_script_dir, build_script)),
            ],
            output_filename = "/output.yml",
            create = create_neurocontainer_emulator,
        ))

        out = json.decode(result.read())

        print(build_script_dir, out["name"], out["version"])

        args.create_output(out["name"] + ".json").write(json.indent(json.encode(out)))
        # print(out)

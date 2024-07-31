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

def parse_neurodocker_package(ret, args, name):
    pkg_args = {}

    index = 1
    for arg in args[1:]:
        if arg.startswith("--"):
            break

        key, _, value = arg.partition("=")
        pkg_args[key] = value
        index += 1

    ret.append(("pkg", name, pkg_args))

    return parse_neurodocker_args(ret, args[index:])

def parse_neurodocker_args(ret, args):
    if len(args) == 0:
        return ret

    if args[0] == "--base-image":
        ret.append(("base-image", args[1]))

        return parse_neurodocker_args(ret, args[2:])
    elif args[0] == "--pkg-manager":
        ret.append(("pkg-manager", args[1]))

        return parse_neurodocker_args(ret, args[2:])
    elif args[0] == "--entrypoint":
        ret.append(("entrypoint", args[1]))

        return parse_neurodocker_args(ret, args[2:])
    elif args[0] == "--workdir":
        ret.append(("workdir", args[1]))

        return parse_neurodocker_args(ret, args[2:])
    elif args[0] == "--user":
        ret.append(("user", args[1]))

        return parse_neurodocker_args(ret, args[2:])
    elif args[0] == "--env":
        index = 1
        for arg in args[1:]:
            if arg.startswith("--"):
                break

            if arg != "":
                ret.append(("env", arg))

            index += 1

        return parse_neurodocker_args(ret, args[index:])
    elif args[0] == "--copy":
        ret.append(("copy", args[1], args[2]))

        return parse_neurodocker_args(ret, args[3:])
    elif args[0] == "--add":
        ret.append(("add", args[1], args[2]))

        return parse_neurodocker_args(ret, args[3:])
    elif args[0] == "--copy-from":
        ret.append(("copy", args[1] + ":" + args[2], args[3]))

        return parse_neurodocker_args(ret, args[4:])
    elif args[0] == "--install":
        index = 1
        for arg in args[1:]:
            if arg.startswith("--"):
                break

            if arg.startswith("opts="):
                index += 1
                continue

            if arg != "":
                ret.append(("install", arg))

            index += 1

        return parse_neurodocker_args(ret, args[index:])
    elif args[0] == "--run":
        ret.append(("run", args[1]))

        return parse_neurodocker_args(ret, args[2:])
    elif args[0].startswith("--run="):
        ret.append(("run", args[0].removeprefix("--run=")))

        return parse_neurodocker_args(ret, args[1:])
    elif args[0].startswith("--run-bash="):
        ret.append(("run", args[0].removeprefix("--run-bash=")))

        return parse_neurodocker_args(ret, args[1:])
    elif args[0].startswith("--workdir="):
        ret.append(("workdir", args[0].removeprefix("--workdir=")))

        return parse_neurodocker_args(ret, args[1:])
    elif args[0].startswith("--user="):
        ret.append(("user", args[0].removeprefix("--user=")))

        return parse_neurodocker_args(ret, args[1:])
    elif args[0].startswith("--install="):
        s = args[0].removeprefix("--install=")

        for pkg in s.split(" "):
            ret.append(("install", s))

        return parse_neurodocker_args(ret, args[1:])
    elif args[0] == "":
        return parse_neurodocker_args(ret, args[1:])
    else:
        for pkg in packages:
            if args[0] == "--" + pkg:
                return parse_neurodocker_package(ret, args, pkg)

        return error("argument not implemented: " + args[0])

def cmd_neurodocker(proc, args):
    args = parse_neurodocker_args([], args[3:])
    proc["/output.yml"] = json.encode({
        "name": proc.env("toolName"),
        "version": proc.env("toolVersion"),
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
    "neurocontainers-master/recipes/brkraw",
    "neurocontainers-master/recipes/cartool",
    "neurocontainers-master/recipes/itksnap",
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

        base_image = [v[1] for v in out["args"] if v[0] == "base-image"]

        # print(build_script_dir, out["name"], out["version"], base_image)
        print(out)

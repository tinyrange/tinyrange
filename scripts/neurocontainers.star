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

def cmd_neurodocker(proc, args):
    proc["/output.yml"] = json.encode(args)

def create_neurocontainer_emulator(emu):
    emu.add_command("wget", cmd_wget)
    emu.add_command("pip", cmd_pip)
    emu.add_command("python", cmd_python)
    emu.add_command("date", cmd_date)
    emu.add_command("neurodocker", cmd_neurodocker)

def main(args):
    files = filesystem(db.build(GIT_REPO))["neurocontainers-master"]

    for recipe in files["recipes"]:
        if type(recipe) != "Directory":
            continue

        build_script = recipe["build.sh"].base
        build_script_dir = recipe["build.sh"].dir

        if build_script_dir == "neurocontainers-master/recipes/bidscoin" or \
           build_script_dir == "neurocontainers-master/recipes/brkraw" or \
           build_script_dir == "neurocontainers-master/recipes/cartool" or \
           build_script_dir == "neurocontainers-master/recipes/civet" or \
           build_script_dir == "neurocontainers-master/recipes/delphi" or \
           build_script_dir == "neurocontainers-master/recipes/diffusiontoolkit" or \
           build_script_dir == "neurocontainers-master/recipes/elastix" or \
           build_script_dir == "neurocontainers-master/recipes/globus" or \
           build_script_dir == "neurocontainers-master/recipes/ilastik" or \
           build_script_dir == "neurocontainers-master/recipes/itksnap" or \
           build_script_dir == "neurocontainers-master/recipes/jamovi" or \
           build_script_dir == "neurocontainers-master/recipes/laynii" or \
           build_script_dir == "neurocontainers-master/recipes/matlab" or \
           build_script_dir == "neurocontainers-master/recipes/mfcsc" or \
           build_script_dir == "neurocontainers-master/recipes/mgltools" or \
           build_script_dir == "neurocontainers-master/recipes/mrsiproc" or \
           build_script_dir == "neurocontainers-master/recipes/relion" or \
           build_script_dir == "neurocontainers-master/recipes/sigviewer" or \
           build_script_dir == "neurocontainers-master/recipes/terastitcher" or \
           build_script_dir == "neurocontainers-master/recipes/topaz" or \
           build_script_dir == "neurocontainers-master/recipes/trackvis" or \
           build_script_dir == "neurocontainers-master/recipes/vesselvio" or \
           build_script_dir == "neurocontainers-master/recipes/vina" or \
           build_script_dir == "neurocontainers-master/recipes/wftfi":
            continue

        print(build_script_dir)

        result = db.build(define.build_emulator(
            directives = [
                GIT_REPO,
                directive.run_command("cd {};neurodocker_buildMode=tinyrange /bin/bash {}".format(build_script_dir, build_script)),
            ],
            output_filename = "/output.yml",
            create = create_neurocontainer_emulator,
        ))

        out = json.decode(result.read())

        print(out)

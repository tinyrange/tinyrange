load("common/shell.star", "register_commands")

def cmd_neurodocker(ctx, args):
    print("neurodocker", args)
    return ""

def cmd_pip(ctx, args):
    print("pip", args)
    return ""

def cmd_tinyrange(ctx, args):
    print("tinyrange", args)
    return ""

def fetch_neurocontainers_repository(ctx, url, ref):
    repo = fetch_git(url)
    ref = repo.branch(ref)

    for folder in ref["recipes"]:
        if type(folder) != "GitTree":
            continue

        if "build.sh" in folder:
            file = folder["build.sh"]
            contents = file.read()

            ctx = shell_context()

            ctx.set_environment("neurodocker_buildMode", "docker")
            ctx.set_environment("neurodocker_buildExt", ".Dockerfile")
            ctx.set_environment("mountPointList", "")
            ctx.set_environment("TINYRANGE", "tinyrange")

            register_commands(ctx)

            ctx.add_command("neurodocker", cmd_neurodocker)
            ctx.add_command("pip", cmd_pip)
            ctx.add_command("tinyrange", cmd_tinyrange)

            ret = ctx.eval(contents)

            print(ret)

    return error("not implemented")

if __name__ == "__main__":
    fetch_repo(fetch_neurocontainers_repository, ("https://github.com/NeuroDesk/neurocontainers", "master"), distro = "neurocontainers")

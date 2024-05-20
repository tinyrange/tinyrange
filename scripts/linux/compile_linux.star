def file_not_found(ctx, filename):
    if filename == "GNUmakefile" or filename == "makefile":
        return None

    if filename == "include/config/auto.conf":
        return None

    print("file_not_found", filename)

    return None

def command_not_found(ctx, args):
    print("command_not_found", args)

    return error("command not found: " + str(args))

def exec_shell(ctx, args):
    if args[1] == "-c":
        print("sh", args)
        result = ctx.eval(args[2], return_stdout = True)
        return result
    else:
        return error("unhandled shell command")

def cmd_uname(ctx, args):
    if len(args) > 1:
        if args[1] == "-m":
            return "x86_64"

    return error("uname not implemented: " + str(args))

def cmd_sed(ctx, args):
    print("sed", args)
    return "x86_64"

def cmd_gcc(ctx, args):
    if len(args) > 1:
        if args[1] == "-print-file-name=include":
            return "/usr/lib/gcc/x86_64-redhat-linux/14/include"

    return error("gcc not implemented: " + str(args))

def cmd_set(ctx, args):
    if len(args) > 1:
        if args[1] == "-e":
            return ""

    return error("set not implemented: " + str(args))

def cmd_cat(ctx, args):
    if len(args) == 2:
        return ctx[args[1]]

    return error("cat not implemented: " + str(args))

def cmd_rm(ctx, args):
    print("rm", args)
    return ""

def cmd_stub(ctx, args):
    print("stub", args)
    return ""

def cmd_make(ctx, args):
    ctx.eval_makefile(list(args[1:])).exec(ctx)

def walk(ctx, graph, dep, prefix = ""):
    print(prefix, dep)

    for depend in dep.depends:
        walk(ctx, graph, depend, prefix = prefix + "  ")

    commands = graph.eval(ctx, dep)
    print(prefix, "eval", commands)

def main(ctx):
    ark = open("local/linux-2.6.39.tar").read_archive(".tar")

    dirname = "linux-2.6.39/"

    ctx = shell_context()

    ctx.set_handlers(
        file_not_found = file_not_found,
        command_not_found = command_not_found,
    )

    ctx.add_command("/bin/sh", exec_shell)
    ctx.add_command("uname", cmd_uname)
    ctx.add_command("sed", cmd_sed)
    ctx.add_command("gcc", cmd_gcc)
    ctx.add_command("set", cmd_set)
    ctx.add_command("cat", cmd_cat)
    ctx.add_command("rm", cmd_rm)
    ctx.add_command("./scripts/gcc-goto.sh", cmd_stub)
    ctx.add_command("make", cmd_make)

    for file in ark:
        name = file.name.removeprefix(dirname)
        if name.endswith("/"):
            name = name.removesuffix("/")
        ctx[name] = file.read()

    ctx["./Makefile"] = ctx["Makefile"]

    graph = ctx.eval_makefile(["kernel"])

    for rule in graph.rules:
        walk(ctx, graph, rule)

if __name__ == "__main__":
    run_script(main)

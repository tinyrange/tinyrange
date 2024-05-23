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
    print("gcc", args)
    args = args[1:]

    if args[0] == "-print-file-name=include":
        return "/usr/lib/gcc/x86_64-redhat-linux/14/include"

    output = ""

    for i in range(len(args)):
        arg = args[i]
        if arg == "-o":
            output = args[i + 1]
        elif "-MD" in arg:
            depname = arg.split(",")[-1]
            print("gcc create depfile", depname)
            ctx[depname] = ""

    if output != "":
        print("gcc create", output)
        ctx[output] = "#!special"
        return ""
    else:
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

def cmd_echo(ctx, args):
    return " ".join(args[1:]) + "\n"

def cmd_fixdep(ctx, args):
    _, depfile, target, cmdline = args

    print("fixdep", depfile, target, cmdline)

    return "fixdep not implemented\n"

def cmd_mv(ctx, args):
    if len(args) == 4 and args[1] == "-f":
        _, _, src, dst = args
        ctx.move(src, dst)

    return error("mv not implemented: " + str(args))

def main(ctx):
    ark = open("local/linux-2.6.39.tar").read_archive(".tar")

    dirname = "linux-2.6.39.4/"

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
    ctx.add_command("echo", cmd_echo)
    ctx.add_command("./scripts/gcc-goto.sh", cmd_stub)
    ctx.add_command("make", cmd_make)
    ctx.add_command("scripts/basic/fixdep", cmd_fixdep)
    ctx.add_command("mv", cmd_mv)

    for file in ark:
        name = file.name.removeprefix(dirname)
        if name.endswith("/"):
            name = name.removesuffix("/")
        ctx[name] = file.read()

    ctx["./Makefile"] = ctx["Makefile"]

    ctx.eval_makefile([]).exec(ctx)

if __name__ == "__main__":
    run_script(main)

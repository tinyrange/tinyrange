DEBUG = False

def cmd_set(ctx, args):
    if args[1] == "-e":
        return ""
    elif args[1] == "-ex":
        return ""
    else:
        return error("unknown set command: " + ", ".join(args))

def cmd_wget(ctx, args):
    if DEBUG:
        print("wget", args[1:])

    if args[1] == "-O-":
        return fetch_http(args[2]).read()
    else:
        return error("unknown wget command: " + ", ".join(args))

def cmd_head(ctx, args):
    if DEBUG:
        print("head", args[1:])
    return ""

def cmd_cut(ctx, args):
    if DEBUG:
        print("cut", args[1:])
    return ""

def cmd_echo(ctx, args):
    if DEBUG:
        print("echo", args[1:])
    return " ".join(args[1:])

def cmd_source(ctx, args):
    if DEBUG:
        print("source", args[1:])
    return ""

def cmd_yes(ctx, args):
    if DEBUG:
        print("yes", args[1:])
    return ""

def cmd_sed(ctx, args):
    if DEBUG:
        print("sed", args[1:])
    return ""

def cmd_touch(ctx, args):
    if DEBUG:
        print("touch", args[1:])
    return ""

def cmd_grep(ctx, args):
    if DEBUG:
        print("grep", args[1:])
    return ""

def cmd_mkdir(ctx, args):
    if DEBUG:
        print("mkdir", args[1:])
    return ""

def cmd_pwd(ctx, args):
    if DEBUG:
        print("pwd", args[1:])
    return "/"

def cmd_true(ctx, args):
    if DEBUG:
        print("true", args[1:])
    return ""

def cmd_false(ctx, args):
    if DEBUG:
        print("false", args[1:])
    return ""

def cmd_dirname(ctx, args):
    if DEBUG:
        print("dirname", args[1:])
    return ""

def cmd_op(ctx, args):
    if args[1] == "-f":
        fname = args[2]

        if fname in ctx:
            return ""

        return "", 1
    elif args[1] == "-d":
        fname = args[2]

        if fname in ctx:
            return ""

        return "", 1
    elif args[1] == "-z":
        if len(args[2]) > 0:
            return "", 1
        else:
            return ""
    elif len(args) == 5 and args[2] == "!=":
        _, a, _, b, _ = args
        if a != b:
            return ""
        else:
            return "", 1

    return error("command [ not implemented: " + str(args))

def cmd_tr(ctx, args):
    if DEBUG:
        print("tr", args[1:])

    return ctx.stdin.replace(args[1], args[2])

def register_commands(ctx):
    ctx.add_command("set", cmd_set)
    ctx.add_command("head", cmd_head)
    ctx.add_command("cut", cmd_cut)
    ctx.add_command("echo", cmd_echo)
    ctx.add_command("source", cmd_source)
    ctx.add_command("yes", cmd_yes)
    ctx.add_command("sed", cmd_sed)
    ctx.add_command("touch", cmd_touch)
    ctx.add_command("grep", cmd_grep)
    ctx.add_command("mkdir", cmd_mkdir)
    ctx.add_command("pwd", cmd_pwd)
    ctx.add_command("true", cmd_true)
    ctx.add_command("false", cmd_false)
    ctx.add_command("dirname", cmd_dirname)
    ctx.add_command("[", cmd_op)
    ctx.add_command("tr", cmd_tr)

    ctx.add_command("wget", cmd_wget)

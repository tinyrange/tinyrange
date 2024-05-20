def cmd_set(ctx, args):
    if args[1] == "-e":
        return ""
    elif args[1] == "-ex":
        return ""
    else:
        return error("unknown set command: " + ", ".join(args))

def cmd_wget(ctx, args):
    if args[1] == "-O-":
        return fetch_http(args[2]).read()
    else:
        return error("unknown wget command: " + ", ".join(args))

def cmd_head(ctx, args):
    # print("head", args)
    return ""

def cmd_cut(ctx, args):
    # print("cut", args)
    return ""

def cmd_echo(ctx, args):
    print("echo", args[1:])
    return ""

def cmd_source(ctx, args):
    # print("source", args)
    return ""

def cmd_yes(ctx, args):
    # print("yes")
    return ""

def cmd_sed(ctx, args):
    # print("sed", args)
    return ""

def cmd_touch(ctx, args):
    return ""

def cmd_grep(ctx, args):
    return ""

def cmd_mkdir(ctx, args):
    return ""

def cmd_pwd(ctx, args):
    return "/"

def cmd_true(ctx, args):
    return ""

def cmd_false(ctx, args):
    return ""

def cmd_dirname(ctx, args):
    return ""

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

    ctx.add_command("wget", cmd_wget)

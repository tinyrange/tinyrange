def cmd_set(ctx, args):
    print("set", args)
    return ""

def cmd_wget(ctx, args):
    print("wget", args)
    return ""

def cmd_head(ctx, args):
    print("head", args)
    return ""

def cmd_cut(ctx, args):
    print("cut", args)
    return ""

def cmd_echo(ctx, args):
    print("echo", args)
    return ""

def cmd_source(ctx, args):
    print("source", args)
    return ""

def cmd_yes(ctx, args):
    print("yes")
    return ""

def cmd_sed(ctx, args):
    print("sed", args)
    return ""

def register_commands(ctx):
    ctx.add_command("set", cmd_set)
    ctx.add_command("head", cmd_head)
    ctx.add_command("cut", cmd_cut)
    ctx.add_command("echo", cmd_echo)
    ctx.add_command("source", cmd_source)
    ctx.add_command("yes", cmd_yes)
    ctx.add_command("sed", cmd_sed)

    ctx.add_command("wget", cmd_wget)

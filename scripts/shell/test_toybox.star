TOYBOX_REPO = "https://github.com/landley/toybox.git"

def cmd_op(ctx, args):
    if args[1] == "-f":
        fname = args[2]

        if fname in ctx:
            return ""

        return "", 1

    return error("command [ not implemented: " + str(args))

def cmd_test(ctx, args):
    if args[0] == "toyonly":
        return cmd_test(ctx, args[1:])
    elif args[0] == "testcmd":
        cmd = ctx.env["C"]

        desc = args[1] if len(args) > 1 else ""
        cmdline = args[2] if len(args) > 2 else ""
        expected = args[3] if len(args) > 3 else ""
        in_file = args[4] if len(args) > 4 else ""
        stdin = args[5] if len(args) > 5 else ""

        return cmd_test(ctx, ("testing", desc, "\"{}\" {}".format(cmd, cmdline), expected, in_file, stdin))
    elif args[0] == "testing":
        if len(args) < 6:
            for _ in range(6 - len(args)):
                args += ("",)

        _, name, command, result, infile, stdin = args

        print("test case", (name, command, result, infile, stdin))

        return ""
    else:
        return error("unknown command: " + args[0])

def add_all(tree, ctx):
    if type(tree) == "GitTree":
        for child in tree:
            add_all(child, ctx)
    elif type(tree) == "File":
        ctx[tree.name] = tree.read()

def main(ctx):
    repo = fetch_git(TOYBOX_REPO).branch("master")

    ctx = shell_context()
    ctx.add_command("[", cmd_op)
    ctx.add_command("testcmd", cmd_test)
    ctx.add_command("testing", cmd_test)
    ctx.add_command("toyonly", cmd_test)

    add_all(repo, ctx)

    for test in [
        repo["tests/base32.test"],
        repo["tests/base64.test"],
        repo["tests/basename.test"],
        repo["tests/echo.test"],
        repo["tests/expr.test"],
        repo["tests/factor.test"],
        repo["tests/printf.test"],
    ]:
        if not test.name.endswith(".test"):
            continue

        print(test.name)

        ctx.set_environment("C", test.name.removesuffix(".test").removeprefix("tests/"))

        ctx.eval(ctx[test.name])

if __name__ == "__main__":
    run_script(main)

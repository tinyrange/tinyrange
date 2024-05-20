load("common/shell.star", "register_commands")

def file_not_found(ctx, filename):
    if filename == "GNUmakefile" or filename == "makefile":
        return None

    return error("file not found: " + filename)

def command_not_found(ctx, args):
    print("command_not_found", args)

    return error("command not found: " + str(args))

def exec_shell(ctx, args):
    if args[1] == "-c":
        ctx.eval(args[2])
        return ""
    else:
        return error("unhandled shell command")

def exec_make(ctx, args):
    if args[1] == "--version":
        return "version"
    else:
        return error("unhandled make command")

def run_test_case(filename, contents):
    print("run_test_case", filename)

    ctx = shell_context()
    register_commands(ctx)

    ctx.add_command("/bin/sh", exec_shell)
    ctx.add_command("make", exec_make)

    ctx.set_handlers(
        file_not_found = file_not_found,
        command_not_found = command_not_found,
    )

    ctx["Makefile"] = contents

    parsed = ctx.eval_makefile(["test"], return_errors = True)
    if type(parsed) == "string":
        print("error: " + parsed)

def main(ctx):
    repo = fetch_git("https://github.com/google/kati").branch("master")

    for testcase in repo["testcase"]:
        if type(testcase) != "File":
            continue
        if not testcase.name.endswith(".mk"):
            continue

        contents = testcase.read()

        run_test_case(testcase.name, contents)

if __name__ == "__main__":
    run_script(main)

OWNER = "SerenityOS"
REPO = "serenity"
COMMIT = "ef766b0b5f9c0a66749abfe7e7636e6a709d1094"

def fetch_github_archive(owner, repo, commit):
    ark = filesystem(fetch_http("https://github.com/{}/{}/archive/{}.tar.gz".format(owner, repo, commit)).read_archive(".tar.gz"))

    return ark["{}-{}".format(repo, commit)]

def cmd_cmake_minimum_required(ctx, args):
    print("cmake_minimum_required", args)
    return None

def cmd_list(ctx, args):
    if args[0] == "APPEND":
        if args[1] not in ctx:
            ctx[args[1]] = args[2]
        else:
            ctx[args[1]] += " " + args[2]
        return None
    else:
        return error("list not implemented" + str(args))

def cmd_project(ctx, args):
    print("project", args)
    return None

def cmd_message(ctx, args):
    if args[0] == "FATAL_ERROR":
        return error("".join(args[1:]))
    else:
        print("message", args)
        return error("message not implemented")

def cmd_set(ctx, args):
    print("set", args)
    ctx[args[0]] = args[1]
    return None

def cmd_find_program(ctx, args):
    print("find_program", args)
    return None

def cmd_add_custom_target(ctx, args):
    print("add_custom_target", args)
    return None

def cmd_configure_file(ctx, args):
    print("configure_file", args)
    return None

def cmd_install(ctx, args):
    print("install", args)
    return None

def cmd_add_compile_options(ctx, args):
    print("add_compile_options", args)
    return None

COMMANDS = {
    "cmake_minimum_required": cmd_cmake_minimum_required,
    "list": cmd_list,
    "project": cmd_project,
    "message": cmd_message,
    "set": cmd_set,
    "find_program": cmd_find_program,
    "add_custom_target": cmd_add_custom_target,
    "configure_file": cmd_configure_file,
    "install": cmd_install,
    "add_compile_options": cmd_add_compile_options,
}

def main(ctx):
    result = eval_cmake(fetch_github_archive(OWNER, REPO, COMMIT), {
        "CMAKE_SYSTEM_NAME": "SerenityOS",
        "CMAKE_CXX_COMPILER_ID": "GNU",
        "CMAKE_CXX_COMPILER_VERSION": "13.2.0",
    }, COMMANDS)

    print(result)

if __name__ == "__main__":
    run_script(main)

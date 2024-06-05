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
            ctx[args[1]] += ";" + args[2]
        return None
    elif args[0] == "PREPEND":
        if args[1] not in ctx:
            ctx[args[1]] = args[2]
        else:
            ctx[args[1]] = args[2] + ";" + ctx[args[1]]
        return None
    elif args[0] == "TRANSFORM":
        target = args[1]
        op = args[2]
        if op == "PREPEND":
            val = args[3]
            ctx[target] = ";".join([val + s for s in ctx[target].split(";")])
            return None
        else:
            return error("list not implemented" + str(args))
    elif args[0] == "REMOVE_ITEM":
        target = args[1]
        item = args[2]
        ctx[target] = ";".join([s for s in ctx[target].split(";") if s != item])
        return None
    elif args[0] == "FILTER":
        print("list", args)
        return None
    else:
        return error("list not implemented" + str(args))

def cmd_project(ctx, args):
    print("project", args)
    return None

def cmd_message(ctx, args):
    if args[0] == "FATAL_ERROR":
        print("fatal", "".join(args[1:]))
        return None
    elif args[0] == "STATUS":
        print("".join(args[1:]))
        return None
    else:
        print("message", args)
        return error("message not implemented")

def cmd_set(ctx, args):
    print("set", args)
    if len(args) > 2 and args[2] == "CACHE":
        ctx[args[0]] = args[1]
        return None
    else:
        ctx[args[0]] = " ".join(args[1:])
        return None

def cmd_unset(ctx, args):
    print("unset", args)
    return None

def cmd_option(ctx, args):
    print("option", args)
    return None

def cmd_define_property(ctx, args):
    print("define_property", args)
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

def cmd_add_link_options(ctx, args):
    print("add_link_options", args)
    return None

def cmd_add_compile_definitions(ctx, args):
    print("add_compile_definitions", args)
    return None

def cmd_include_directories(ctx, args):
    print("include_directories", args)
    return None

def cmd_add_dependencies(ctx, args):
    print("add_dependencies", args)
    return None

def cmd_find_package(ctx, args):
    print("find_package", args)
    return None

def cmd_add_library(ctx, args):
    print("add_library", args)
    return None

def cmd_add_custom_command(ctx, args):
    print("add_custom_command", args)
    return None

def cmd_target_link_libraries(ctx, args):
    print("target_link_libraries", args)
    return None

def cmd_add_executable(ctx, args):
    print("add_executable", args)
    return None

def cmd_execute_process(ctx, args):
    print("execute_process", args)
    return None

def cmd_add_definitions(ctx, args):
    print("add_definitions", args)
    return None

def cmd_target_sources(ctx, args):
    print("target_sources", args)
    return None

def cmd_target_compile_definitions(ctx, args):
    print("target_compile_definitions", args)
    return None

def cmd_target_compile_options(ctx, args):
    print("target_compile_options", args)
    return None

def cmd_target_link_options(ctx, args):
    print("target_link_options", args)
    return None

def cmd_target_link_directories(ctx, args):
    print("target_link_directories", args)
    return None

def cmd_link_directories(ctx, args):
    print("link_directories", args)
    return None

def cmd_set_source_files_properties(ctx, args):
    print("set_source_files_properties", args)
    return None

def cmd_set_target_properties(ctx, args):
    print("set_target_properties", args)
    return None

def cmd_get_property(ctx, args):
    print("get_property", args)
    var = args[0]
    ctx[var] = ""
    return None

def cmd_set_property(ctx, args):
    print("set_property", args)
    return None

def cmd_get_target_property(ctx, args):
    var = args[0]
    target = args[1]
    property = args[2]
    print("get_target_property", args)
    ctx[var] = ""
    return None

def cmd_file(ctx, args):
    print("file", args)
    return None

def cmd_string(ctx, args):
    print("string", args)
    return None

def cmd_cmake_parse_arguments(ctx, args):
    print("cmake_parse_arguments", args)
    return None

def cmd_cmake_path(ctx, args):
    print("cmake_path", args)
    return None

def cmd_get_filename_component(ctx, args):
    print("get_filename_component", args)
    return None

COMMANDS = {
    "cmake_minimum_required": cmd_cmake_minimum_required,
    "list": cmd_list,
    "project": cmd_project,
    "message": cmd_message,
    "set": cmd_set,
    "unset": cmd_unset,
    "option": cmd_option,
    "define_property": cmd_define_property,
    "find_program": cmd_find_program,
    "add_custom_target": cmd_add_custom_target,
    "add_custom_command": cmd_add_custom_command,
    "configure_file": cmd_configure_file,
    "install": cmd_install,
    "add_compile_options": cmd_add_compile_options,
    "add_compile_definitions": cmd_add_compile_definitions,
    "add_link_options": cmd_add_link_options,
    "include_directories": cmd_include_directories,
    "add_dependencies": cmd_add_dependencies,
    "find_package": cmd_find_package,
    "add_library": cmd_add_library,
    "add_executable": cmd_add_executable,
    "execute_process": cmd_execute_process,
    "add_definitions": cmd_add_definitions,
    # Target
    "target_sources": cmd_target_sources,
    "target_link_libraries": cmd_target_link_libraries,
    "target_compile_definitions": cmd_target_compile_definitions,
    "target_compile_options": cmd_target_compile_options,
    "target_link_options": cmd_target_link_options,
    "target_link_directories": cmd_target_link_directories,
    "link_directories": cmd_link_directories,
    "set_source_files_properties": cmd_set_source_files_properties,
    "set_target_properties": cmd_set_target_properties,
    "get_property": cmd_get_property,
    "set_property": cmd_set_property,
    "get_target_property": cmd_get_target_property,
    "file": cmd_file,
    "string": cmd_string,
    "cmake_parse_arguments": cmd_cmake_parse_arguments,
    "cmake_path": cmd_cmake_path,
    "get_filename_component": cmd_get_filename_component,
}

def main(ctx):
    result = eval_cmake(fetch_github_archive(OWNER, REPO, COMMIT), {
        "CMAKE_SYSTEM_NAME": "SerenityOS",
        "CMAKE_CXX_COMPILER_ID": "GNU",
        "CMAKE_CXX_COMPILER_VERSION": "13.2.0",
        "SERENITY_ARCH": "x86_64",
    }, COMMANDS)

    print(result)

if __name__ == "__main__":
    run_script(main)

load_fetcher("//fetchers/alpine.star")

def make_source_archive(ctx):
    fs = filesystem()

    fs["tinyrange/"] = filesystem(db.get_builtin_executable("source", ""))

    return ctx.archive(fs)

directives = [
    define.plan(
        builder = "alpine@3.20",
        packages = [
            query("busybox"),
            query("busybox-binsh"),
            query("alpine-baselayout"),
            query("go"),
            query("curl"),
        ],
        tags = ["level3"],
    ),
    define.build(make_source_archive),
    directive.run_command("cd /tinyrange;go build -o /tinyrange/tinyrange ."),
]

tinyrange_exe = define.build_vm(
    directives = directives,
    storage_size = 2048,
    memory_mb = 2048,
    output = "/tinyrange/tinyrange",
)

load_fetcher("fetchers/alpine.star")

def main(args):
    directives = [
        define.plan(
            builder = "alpine@3.20",
            packages = [
                query("busybox"),
                query("busybox-binsh"),
                query("alpine-baselayout"),
            ],
            tags = ["level3"],
        ),
        directive.run_command("interactive"),
    ]

    # Run the virtual machine using TinyRange.
    # The final run_command makes it interactive.
    db.build(
        define.build_vm(
            directives = directives,
        ),
        always_rebuild = True,
    )

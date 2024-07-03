load_fetcher("fetchers/alpine.star")

def main(args):
    builder = db.builder("alpine@3.20")

    package_list = [
        query("busybox"),
        query("busybox-binsh"),
        query("alpine-baselayout"),
    ]

    # Run the virtual machine using TinyRange.
    # The final run_command makes it interactive.
    db.build(
        define.build_vm(
            directives = builder.plan(
                packages = package_list,
                tags = ["level3"],
            ).directives + [directive.run_command("interactive")],
            storage_size = 4096,
        ),
        always_rebuild = True,
    )

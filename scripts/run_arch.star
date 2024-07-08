load_fetcher("//fetchers/arch.star")

def main(args):
    builder = db.builder("archlinux")

    package_list = [
        query("gcc"),
    ]

    # Run the virtual machine using TinyRange.
    # The final run_command makes it interactive.
    db.build(
        define.build_vm(
            directives = builder.plan(
                packages = package_list,
                tags = ["level3"],
            ).directives + [
                directive.run_command("/bin/bash"),
            ],
        ),
        always_rebuild = True,
    )

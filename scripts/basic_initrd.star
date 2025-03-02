INIT_C = """
#include <stdio.h>
#include <stdbool.h>

int main() {
    printf("Hello, world!\\n");
    return 0x1337;
}
"""

simple_init = define.build_vm(
    directives = [
        define.plan(
            builder = "alpine@3.21",
            packages = [
                query("build-base"),
            ],
            tags = ["level3", "defaults"],
        ),
        directive.add_file("/root/init.c", file(INIT_C)),
        directive.run_command("gcc -static -o /root/init /root/init.c"),
    ],
    output = "/root/init",
)

init_script = file("""
def main():
    print("Hello, world!")
""")

planned_init = define.build_fs(
    directives = [
        directive.builtin("init", "init"),
        directive.add_file("init.star", init_script),
    ],
    kind = "initramfs",
)

init = define.build_fs(
    directives = [
        directive.add_file("init", simple_init),
    ],
    kind = "initramfs",
)

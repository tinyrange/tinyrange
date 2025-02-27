INIT_C = """
#include <stdio.h>
#include <stdbool.h>

int main() {
    while (true) {
        fprintf(stdout, "Hello, world!\\n");
    }
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

init = define.build_fs(
    directives = [
        directive.add_file("init", simple_init),
    ],
    kind = "initramfs",
)

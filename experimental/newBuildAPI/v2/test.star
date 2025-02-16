alpine = fetcher("alpine")

C_SOURCE = file("hello.c", """
#include <stdio.h>

int main() {
  printf("Hello, World\n");
  return 0;
}
""")

def _compile_c(ctx, *sources, cflags=None):
    root_fs = alpine.latest.build_base
    root_fs += (directory("sources") + sources)
    return build_vm(root_fs).run(
        ["gcc"] + (cflags or []) + root_fs.glob("/sources/*.c"),
    ).output("/a.out")

compile_c = build(_compile_c)

out = compile_c(C_SOURCE).default

test_vm = build_vm(
    alpine.latest + (directory("bin") + out.rename("test")),
).interactive()

def main(db):
    db.build(test_vm)
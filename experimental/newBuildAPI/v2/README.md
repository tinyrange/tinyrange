# A New Build API for TinyRange

The core purpose of the build system in TinyRange is to construct filesystems or fragments of them. The current API makes this a bit challenging to do. I'd like to explore what a new API could look like from the perspective of Starlark scripting.

## Examples

### Compiling C Code

Let's try building a simple C source file and including it in a new VM.

```python
load("//fetchers/alpine", "alpine")

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
        "gcc", *(cflags or []), *root_fs.glob("/sources/*.c"),
    ).output("/a.out")

compile_c = build(_compile_c)

out = compile_c(C_SOURCE)

test_vm = build_vm(
    alpine.latest + (directory("bin") + out.rename("test")),
).interactive()
```

The API makes heavy use of overloaded operations and some slightly unintuitive object shapes to simplify the code.

For example `build_vm` returns a `BuildVMDefinition` and takes a list of fragments. BuildVMDefinition can have more fragments appending on the end along with helper methods for adding simple fragments like `run` which runs a command inside the VM. `output` returns a terminal version of the definition with a output filename specified. The terminal version can then be used as a file in other calls.

`alpine` is a alpine fetcher with a series of exported members like `latest`. Using the .dot notation adds additional packages like `build_base` (which are normalized into valid Starlark identifiers but `alpine.latest["hello"]` can be used to query using original package names). These can be cast into fragments which is how they're used in both cases.

The `glob` command is a little tricky. It's not actually performing the glob eagerly but rather represents a query against the filesystem that will later be executed returning a list of strings.
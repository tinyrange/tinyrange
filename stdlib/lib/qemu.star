QEMU_STAR = """
def main():
    magic = "\\\\x7fELF\\\\x02\\\\x01\\\\x01\\\\x00\\\\x00\\\\x00\\\\x00\\\\x00\\\\x00\\\\x00\\\\x00\\\\x00\\\\x02\\\\x00\\\\x3e\\\\x00"
    mask = "\\\\xff\\\\xff\\\\xff\\\\xff\\\\xff\\\\xfe\\\\xfe\\\\x00\\\\xff\\\\xff\\\\xff\\\\xff\\\\xff\\\\xff\\\\xff\\\\xff\\\\xfe\\\\xff\\\\xff\\\\xff"
    register_string = ":{}:M::{}:{}:{}:{}".format("qemu-x86_64", magic, mask, "/.pkg/qemu-x86_64", "OCF")
    mount("binfmt_misc", "binfmt_misc", "/proc/sys/fs/binfmt_misc")
    file_write("/proc/sys/fs/binfmt_misc/register", register_string)

"""

def qemu_download(ctx, arch):
    pkg_def = define.plan(
        builder = "alpine@3.20",
        arch = arch,
        packages = [
            query("qemu-x86_64"),
        ],
        tags = ["download"],
    )

    pkg = ctx.build(pkg_def)

    pkg_fs, _ = pkg.filesystem()

    ret = filesystem()

    ret["/.pkg/qemu-x86_64"] = pkg_fs["/usr/bin/qemu-x86_64"]
    ret["/.pkg/qemu-x86_64.star"] = file(QEMU_STAR)

    return ctx.archive(ret)

test_vm = define.build_vm(
    directives = [
        define.build(qemu_download, "aarch64"),
        directive.run_command("/init -star /.pkg/qemu-x86_64.star"),
        define.plan(
            builder = "alpine@3.20",
            arch = "x86_64",
            packages = [],
            tags = ["level3", "defaults"],
        ),
        directive.run_command("interactive"),
    ],
) 

def user(arch):
    "#macro variable,arch"
    if arch == "x86_64":
        return directive.list([])

    return directive.list([
        define.build(qemu_download, arch),
        directive.run_command("/init -star /.pkg/qemu-x86_64.star"),
    ])
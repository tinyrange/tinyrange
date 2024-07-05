INIT_STAR = """
def main():
    print("Hello, World")
"""

def main(args):
    init_fs = filesystem()

    init_fs["init"] = file(args["init"], executable = True)
    init_fs["init.star"] = INIT_STAR

    fs = define.build_fs(
        directives = [define.archive(init_fs)],
        kind = "initramfs",
    )

    vm = define.build_vm(
        initramfs = fs,
    )

    db.build(vm, always_rebuild = True)

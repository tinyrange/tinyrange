def walk(ent, prefix = ""):
    print(prefix, ent)
    if type(ent) == "Directory":
        for child in ent:
            walk(child, prefix = prefix + "  ")

def main(args):
    if "in" in args:
        fs = filesystem(
            db.build(
                define.read_archive(args["in"], ".tar.gz"),
                memory = True,
            ),
        )

        walk(fs)
    else:
        fs = filesystem()
        fs["hello.txt"] = file("Hello, World", executable = True)

        fs["testing/hello2.txt"] = file("Hello, World 2")

        for k in fs:
            print(k)

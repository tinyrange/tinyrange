hello_url = "https://ftp.gnu.org/gnu/hello/hello-2.12.tar.gz"

hello_archive_def = define.read_archive(define.fetch_http(hello_url), ".tar.gz")

def cmd_sh(ctx, args):
    print("sh", args)

def main(args):
    fs = filesystem()

    fs["/bin/sh"] = program("sh", cmd_sh)

    fs["/root/hello"] = filesystem(db.build(hello_archive_def))["hello-2.12"]

    emu = emulator(fs)

    emu.run("./configure", cwd = "/root/hello")

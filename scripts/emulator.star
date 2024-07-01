hello_url = "https://ftp.gnu.org/gnu/hello/hello-2.12.tar.gz"

hello_archive_def = define.read_archive(define.fetch_http(hello_url), ".tar.gz")

def main(args):
    fs = filesystem()

    fs["/root/hello"] = filesystem(db.build(hello_archive_def))["hello-2.12"]

    emu = emulator(fs)

    emu.add_shell_utilities()

    emu.run("/root/hello/configure", cwd = "/root/hello", env = {
        "PATH": "/bin",
        "PATH_SEPARATOR": ":",
        "HOME": "/root",
        "UID": "0",
        "EUID": "0",
        "GID": "0",
    })

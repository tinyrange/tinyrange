STDLIB_URL = "https://github.com/Afirium/zigc-wasm/releases/download/v0.11.0/std.zip"
ZIG_URL = "https://github.com/Afirium/zigc-wasm/releases/download/v0.11.0/zig_small.wasm"

def main(args):
    fs = filesystem()

    fs["/lib"] = filesystem(db.build(
        define.read_archive(
            define.fetch_http(STDLIB_URL),
            ".zip",
        ),
    ))

    fs["/zigc"] = db.build(
        define.fetch_http(ZIG_URL),
    )

    fs["/input.c"] = """
int main() {return 0;}
"""

    emu = emulator(fs)

    emu.run("/zigc build-exe /input.c")

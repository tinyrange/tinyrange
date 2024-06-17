def main(args):
    fs = filesystem()

    fs["/hello"] = args["hello.wasm"]

    emu = emulator(fs)

    emu.run("/hello")

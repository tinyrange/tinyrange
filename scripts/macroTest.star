def hello(target):
    "#macro string"
    print("Hello, {}".format(target))
    return None

def pixi(default):
    "#macro builder,default"
    return directive.list(
        directive.add_package("bash"),
        define.build_vm(
            [
                default.add_package("bash"),
                directive.run_command("curl -fsSL https://pixi.sh/install.sh | bash"),
            ],
            output = "/init/changed.archive",
        ),
    )

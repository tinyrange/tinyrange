def hello(target):
    "#macro string"
    print("Hello, {}".format(target))
    return None

def pixi(default):
    "#macro builder,default"
    return directive.list([
        directive.add_package(query("bash")),
        directive.add_package(query("curl")),
        directive.archive(define.build_vm(
            [
                default.with_packages([query("bash"), query("curl")]),
                directive.environment({
                    "HOME": "/root",
                }),
                directive.run_command("set -e;curl -fsSL https://pixi.sh/install.sh | bash"),
            ],
            output = "/init/changed.archive",
        )),
        directive.run_command("echo \"PATH=/root/.pixi/bin:\\$PATH\" >> /root/.profile"),
    ])

def uv(default):
    "#macro builder,default"
    return directive.list([
        directive.archive(define.build_vm(
            [
                default.with_packages([query("curl")]),
                directive.environment({
                    "HOME": "/root",
                }),
                directive.run_command("curl -LsSf https://astral.sh/uv/install.sh | sh"),
            ],
            output = "/init/changed.archive",
        )),
    ])

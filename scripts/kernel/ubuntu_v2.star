load("repos/debian.star", "add_ubuntu_fetchers")
load("scripts/kernel/common.star", "parse_config_file")

def main(ctx):
    # Pulls less dependancies than linux-image-generic.
    kernel_packages = ctx.query(name(name = "linux-image-virtual"))

    print(kernel_packages)

    for pkg in kernel_packages:
        plan = ctx.plan(pkg, recommends = False)

        fs = ctx.download_all(*plan)

        vmlinuz = None
        config = None
        modules = []

        for f in fs:
            if f.name.startswith("./boot/vmlinuz-"):
                vmlinuz = f
            elif f.name.startswith("./boot/config-"):
                config = parse_config_file(f.read())
            elif f.name.startswith("./lib/modules/"):
                modules.append(f)

        print(pkg)
        # print(vmlinuz, config, modules)

    return None

if __name__ == "__main__":
    add_ubuntu_fetchers(only_latest = False)

    run_script(main)

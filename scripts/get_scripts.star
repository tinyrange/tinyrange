def get_scripts(ctx, plan):
    fs, _ = plan.filesystem()
    out = filesystem()
    out[".pkg"] = fs[".pkg"]

    return ctx.archive(out)

plan = define.plan(
    builder = "alpine@3.20",
    packages = [
        query("openrc"),
        query("tigervnc"),
        query("supervisor"),
        query("dbus"),
        query("dbus-x11"),
        query("dbus-openrc"),
        query("xfce4"),
        query("xfce4-terminal"),
        query("adwaita-icon-theme"),
        query("faenza-icon-theme"),
    ],
    tags = ["level3", "defaults"],
)

scripts = define.build(get_scripts, plan)

script_fs = define.build_fs([scripts], "tar")

plan_ubuntu = define.plan(
    builder = "ubuntu@jammy",
    packages = [
        query("build-essential"),
    ],
    tags = ["level3", "defaults"],
)

scripts_ubuntu = define.build(get_scripts, plan_ubuntu)

script_ubuntu_fs = define.build_fs([scripts_ubuntu], "tar")

plan_ubuntu_ui = define.plan(
    builder = "ubuntu@jammy",
    packages = [
        query("xfce4"),
    ],
    tags = ["level3", "defaults"],
)

scripts_ubuntu_ui = define.build(get_scripts, plan_ubuntu_ui)

script_ubuntu_ui_fs = define.build_fs([scripts_ubuntu_ui], "tar")

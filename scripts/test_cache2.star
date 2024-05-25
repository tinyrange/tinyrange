load("repos/alpine.star", "add_alpine_fetchers")

def make_fs(ark):
    fs = filesystem()

    for file in ark:
        if file.name.endswith("/"):
            continue
        fs[file.name] = file

    return fs

def parse_pkg_info(contents):
    ret = {}

    for line in contents.splitlines():
        if line.startswith("#"):
            continue
        k, v = line.split(" = ", 1)
        ret[k] = v

    return ret

def build_package_archive(ctx, pkg):
    fs = filesystem()

    ark = ctx.db.download(pkg)

    pkg_info = None

    for file in ark:
        if file.name.startswith("."):
            if file.name == ".PKGINFO":
                pkg_info = parse_pkg_info(file.read())
                continue
            elif file.name.startswith(".SIGN.RSA"):
                # TODO(joshua): Validate signatures.
                continue
            elif file.name == ".pre-install":
                fs[".pkg/apk/pre-install/" + pkg.name + ".sh"] = file
                continue
            elif file.name == ".post-install":
                fs[".pkg/apk/post-install/" + pkg.name + ".sh"] = file
                continue
            elif file.name == ".pre-upgrade":
                # Not needed since we are always installing not upgrading.
                continue
            elif file.name == ".post-upgrade":
                # Not needed since we are always installing not upgrading.
                continue
            elif file.name == ".trigger":
                fs[".pkg/apk/trigger/" + pkg.name + ".txt"] = json.encode(pkg_info["triggers"].split(" "))
                fs[".pkg/apk/trigger/" + pkg.name + ".sh"] = file
                continue
            elif file.name == ".dummy":
                continue
            else:
                return error("hidden file not implemented: " + file.name)
        if file.name.endswith("/"):
            continue  # ignore directoriess
        fs[file.name] = file

    return ctx.archive(fs)

def build_rootfs_from_plan(ctx, plan):
    fs = filesystem()

    for pkg in plan:
        f = ctx.db.build((__file__, pkg), build_package_archive, (pkg,))

        fs += make_fs(f.read_archive(".tar"))

    return ctx.archive(fs)

def main(ctx):
    plan = ctx.plan(name("build-base"), name("alpine-baselayout"), name("busybox"))

    f = ctx.build((__file__, plan), build_rootfs_from_plan, (plan,))

    print(get_cache_filename(f))

if __name__ == "__main__":
    add_alpine_fetchers(only_latest = True)
    run_script(main)

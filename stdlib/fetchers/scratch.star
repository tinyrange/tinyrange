def noop():
    pass

def build_scratch_directives(builder, plan):
    return []

if __name__ == "__main__":
    for arch in ["x86_64", "aarch64"]:
        db.add_container_builder(define.container_builder(
            name = "scratch",
            arch = arch,
            display_name = "scratch",
            plan_callback = build_scratch_directives,
            packages = define.package_collection(noop, noop),
        ))

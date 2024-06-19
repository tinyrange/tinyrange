ALPINE_IMAGE = define.fetch_oci_image("library/alpine")

def main(args):
    step = define.build_vm(
        directives = [
            ALPINE_IMAGE,
            directive.run_command("echo \"hello world\" > /root/world"),
        ],
        output = "/root/world",
    )

    res = db.build(step)

    print(res.read())

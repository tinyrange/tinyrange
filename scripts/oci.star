def walk(ent, prefix = ""):
    print(prefix, ent)
    if type(ent) == "Directory":
        for child in ent:
            walk(child, prefix = prefix + "  ")

def main(args):
    fs = db.build(define.fetch_oci_image(
        image = "library/ubuntu",
        tag = "latest",
        arch = "amd64",
    ))
    walk(fs)

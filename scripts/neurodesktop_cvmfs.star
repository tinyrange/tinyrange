version = "2025-03-12"

def main(args):
    for arch in ["amd64", "arm64"]:
        db.build(define.fetch_oci_image(
            image = "vnmd/neurodesktop",
            tag = version,
            arch = arch,
        ))
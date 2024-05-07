def parse_deb(f):
    archive = f.read_archive(".ar")

    control = None
    data = None

    for f in archive:
        if f.name.startswith("control."):
            control = f.read_archive(f.name)
        elif f.name.startswith("data."):
            data = f.read_archive(f.name)

    return control, data

def parse_config_file(contents):
    lines = contents.splitlines()

    ret = {}

    for line in lines:
        if len(line) == 0 or line.startswith("#"):
            continue
        k, _, v = line.partition("=")

        ret[k] = v.lower()

    return ret

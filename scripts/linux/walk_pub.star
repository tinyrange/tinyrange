directory_regex = re.compile('<a href="(.*)">(.*)<\\/a>\\s+([0-9]{2}-[a-zA-Z]{3}-[0-9]{4} [0-9]{2}:[0-9]{2})\\s+(.*)\r\n')

version_regex = re.compile("v[0-9]+.[0-9x]/")

linux_archive = re.compile("linux-([0-9]+)\\.([0-9]+)\\.([0-9]+)\\.tar\\.xz")

def get_directory_items(url, **kwargs):
    resp = fetch_http(url, **kwargs)
    if resp == None:
        return error("not found")

    contents = resp.read()

    ret = []

    for _, name, _, modified, size in directory_regex.find_all_submatch(contents):
        ret.append((url + name, modified, size))

    return ret

def main(ctx):
    versions = {}

    # Get the latest patch of every avalible Linux version.
    for name, _, _ in get_directory_items("https://cdn.kernel.org/pub/linux/kernel/"):
        if not version_regex.matches(name):
            continue
        for name, _, _ in get_directory_items(name):
            if not linux_archive.matches(name):
                continue
            filename = name.split("/")[-1]
            _, major, minor, patch = linux_archive.find_submatch(filename)
            key = (int(major), int(minor))
            patch = int(patch)
            if key in versions:
                _, _, _, existing_patch = linux_archive.find_submatch(versions[key])
                existing_patch = int(existing_patch)
                if existing_patch > patch:
                    continue

            versions[key] = name

    for k in sorted(versions):
        major, minor = k
        print(major, minor, versions[k])

if __name__ == "__main__":
    run_script(main)

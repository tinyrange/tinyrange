directory_regex = re.compile('<a href="(.*)">(.*)<\\/a>\\s+([0-9]{2}-[a-zA-Z]{3}-[0-9]{4} [0-9]{2}:[0-9]{2})\\s+(.*)\r\n')

version_regex = re.compile("v[0-9]+.[0-9x]/")

linux_archive = re.compile("linux-[0-9]+\\.[0-9]+\\.[0-9]+\\.tar\\.xz")

def get_directory_items(url):
    resp = fetch_http(url)
    if resp == None:
        return error("not found")

    contents = resp.read()

    ret = []

    for _, name, _, modified, size in directory_regex.find_all_submatch(contents):
        ret.append((url + name, modified, size))

    return ret

def main(ctx):
    for name, modified, size in get_directory_items("https://cdn.kernel.org/pub/linux/kernel/"):
        if not version_regex.matches(name):
            continue
        for name, modified, size in get_directory_items(name):
            if not linux_archive.matches(name):
                continue
            print(name, modified, size)

if __name__ == "__main__":
    run_script(main)

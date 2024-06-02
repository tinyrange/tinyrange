OWNER = "SerenityOS"
REPO = "serenity"
COMMIT = "ef766b0b5f9c0a66749abfe7e7636e6a709d1094"

def fetch_github_archive(owner, repo, commit):
    ark = filesystem(fetch_http("https://github.com/{}/{}/archive/{}.tar.gz".format(owner, repo, commit)).read_archive(".tar.gz"))

    return ark["{}-{}".format(repo, commit)]

def walk_all_cmake(d):
    # print(d)

    for child in d:
        if type(child) == "Directory":
            walk_all_cmake(child)
        elif child.base == "CMakeLists.txt":
            ast = parse_cmake(child)

            print(child, ast)
        else:
            continue

def main(ctx):
    walk_all_cmake(fetch_github_archive(OWNER, REPO, COMMIT))

if __name__ == "__main__":
    run_script(main)

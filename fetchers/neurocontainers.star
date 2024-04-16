def fetch_neurocontainers_repository(ctx, url, ref):
    repo = fetch_git(url)
    ref = repo.branch(ref)

    for folder in ref["recipes"]:
        if type(folder) != "GitTree":
            continue

        if "build.sh" in folder:
            file = folder["build.sh"]
            contents = file.read()
            print(contents)
            ret = eval_shell(contents)
            print(ret)

    return error("not implemented")

if __name__ == "__main__":
    fetch_repo(fetch_neurocontainers_repository, ("https://github.com/NeuroDesk/neurocontainers", "master"), distro = "neurocontainers")

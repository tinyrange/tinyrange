def fetch_easybuild_repo(ctx, url, branch):
    repo = fetch_git(url)
    tree = repo.branch(branch)["easybuild/easyconfigs"]

    for letter in tree:
        if len(letter.base) > 1:
            continue
        for pkg in letter:
            for file in pkg:
                if not file.name.endswith(".eb"):
                    continue

                contents = file.read()

                print(file)
                print(contents)

                contents = contents.replace("local_verdir = ''.join(char for char in version if not char.isalpha())", "local_verdir = ''.join(char for char in version.elems() if not char.isalpha())")

                obj = eval_python(
                    contents,
                    GNU_SOURCE = "GNU_SOURCE",
                    GNU_SAVANNAH_SOURCE = "GNU_SAVANNAH_SOURCE",
                    SOURCE_ZIP = "SOURCE_ZIP",
                    SOURCE_TAR = "SOURCE_TAR",
                    SOURCE_TGZ = "SOURCE_TGZ",
                    SOURCE_WHL = "SOURCE_WHL",
                    SOURCE_PY3_WHL = "SOURCE_PY3_WHL",
                    SOURCE_TAR_GZ = "SOURCE_TAR_GZ",
                    SOURCE_TAR_XZ = "SOURCE_TAR_XZ",
                    SOURCE_TAR_BZ2 = "SOURCE_TAR_BZ2",
                    SOURCELOWER_ZIP = "SOURCELOWER_ZIP",
                    SOURCELOWER_TGZ = "SOURCELOWER_TGZ",
                    SOURCELOWER_TAR_GZ = "SOURCELOWER_TAR_GZ",
                    SOURCELOWER_TAR_XZ = "SOURCELOWER_TAR_XZ",
                    SOURCELOWER_TAR_BZ2 = "SOURCELOWER_TAR_BZ2",
                    SOURCEFORGE_SOURCE = "SOURCEFORGE_SOURCE",
                    GITHUB_SOURCE = "GITHUB_SOURCE",
                    GITHUB_LOWER_SOURCE = "GITHUB_LOWER_SOURCE",
                    PYPI_SOURCE = "PYPI_SOURCE",
                    PYPI_LOWER_SOURCE = "PYPI_LOWER_SOURCE",
                    FTPGNOME_SOURCE = "FTPGNOME_SOURCE",
                    GITHUB_RELEASE = "GITHUB_RELEASE",
                    GITHUB_LOWER_RELEASE = "GITHUB_LOWER_RELEASE",
                    BITBUCKET_DOWNLOADS = "BITBUCKET_DOWNLOADS",
                    BITBUCKET_SOURCE = "BITBUCKET_SOURCE",
                    XORG_PROTO_SOURCE = "XORG_PROTO_SOURCE",
                    XORG_LIB_SOURCE = "XORG_LIB_SOURCE",
                    XORG_UTIL_SOURCE = "XORG_UTIL_SOURCE",
                    SYSTEM = "SYSTEM",
                    OS_TYPE = "Linux",
                    ARCH = "x86_64",
                    SHLIB_EXT = "SHLIB_EXT",
                    HOME = "HOME",
                    OS_PKG_OPENSSL_DEV = "OS_PKG_OPENSSL_DEV",
                    OS_PKG_IBVERBS_DEV = "OS_PKG_IBVERBS_DEV",
                    EXTERNAL_MODULE = "EXTERNAL_MODULE",
                    SYS_PYTHON_VERSION = "SYS_PYTHON_VERSION",
                )

                print(obj)

    return error("not implemented")

if __name__ == "__main__":
    fetch_repo(fetch_easybuild_repo, ("https://github.com/easybuilders/easybuild-easyconfigs", "develop"), distro = "easybuild")

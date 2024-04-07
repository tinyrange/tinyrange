load("fetchers/arch.star", "fetch_arch_repository", "fetch_aur_repository")

arch_mirror = "https://mirror.aarnet.edu.au/pub/archlinux"

def add_arch_fetchers():
    for pool in ["core", "community", "extra", "multilib"]:
        for arch in ["x86_64"]:
            fetch_repo(
                fetch_arch_repository,
                (
                    "{}/{}/os/{}".format(arch_mirror, pool, arch),
                    pool,
                    True,
                ),
                distro = "arch",
            )

def add_bioarch_fetchers():
    fetch_repo(fetch_arch_repository, (
        "https://repo.bioarchlinux.org/x86_64",
        "bioarchlinux",
        False,
    ), distro = "arch")

def add_aur_fetchers():
    fetch_repo(fetch_aur_repository, (), distro = "arch")

if __name__ == "__main__":
    add_arch_fetchers()
    add_bioarch_fetchers()
    add_aur_fetchers()

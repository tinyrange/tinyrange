load("fetchers/cran.star", "fetch_cran_repostiory")

def add_cran_fetchers(only_latest = True):
    # Source Version
    fetch_repo(
        fetch_cran_repostiory,
        ("https://cran.r-project.org/src/contrib",),
        distro = "cran",
    )

if __name__ == "__main__":
    add_cran_fetchers(only_latest = False)

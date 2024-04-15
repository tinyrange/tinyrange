load("fetchers/conda.star", "fetch_conda_repository")

def add_conda_fetchers():
    fetch_repo(fetch_conda_repository, ("https://repo.anaconda.com/pkgs/main/linux-64/", "x86_64"), distro = "conda")
    fetch_repo(fetch_conda_repository, ("https://repo.anaconda.com/pkgs/main/noarch/", ""), distro = "conda")

    # pkgs/free
    fetch_repo(fetch_conda_repository, ("https://repo.anaconda.com/pkgs/free/linux-64/", "x86_64"), distro = "conda")

    # Menpo
    fetch_repo(fetch_conda_repository, ("https://conda.anaconda.org/menpo/linux-64/", "x86_64"), distro = "conda")

    # Conda Forge
    fetch_repo(fetch_conda_repository, ("https://conda.anaconda.org/conda-forge/linux-64/", "x86_64"), distro = "conda")

    # Bioconda
    fetch_repo(fetch_conda_repository, ("https://conda.anaconda.org/bioconda/linux-64/", "x86_64"), distro = "conda")

    # Anaconda Fusion
    fetch_repo(fetch_conda_repository, ("https://conda.anaconda.org/anaconda-fusion/linux-64/", "x86_64"), distro = "conda")

if __name__ == "__main__":
    add_conda_fetchers()

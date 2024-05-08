load("fetchers/debian.star", "fetch_debian_repository", "fetch_debian_repository_v2")

ubuntu_mirror = "http://au.archive.ubuntu.com/ubuntu"
old_ubuntu_fallback = "http://old-releases.ubuntu.com/ubuntu"

debian_mirror = "http://mirror.aarnet.edu.au/pub/debian"
old_debian_fallback = "http://archive.debian.org/debian"

kali_mirror = "https://mirror.aarnet.edu.au/pub/kali/kali"

neurodebian_mirror = "https://mirror.aarnet.edu.au/pub/neurodebian"

ubuntu_versions = {
    "mantic": "23.10",
    "lunar": "23.04",
    "kinetic": "22.10",
    "jammy": "22.04",
    "impish": "21.10",
    "hirsute": "21.04",
    "groovy": "20.10",
    "focal": "20.04",
    "eoan": "19.10",
    "disco": "19.04",
    "cosmic": "18.10",
    "bionic": "18.04",
    "artful": "17.10",
    "zesty": "17.04",
    "yakkety": "16.10",
    "xenial": "16.04",
    "wily": "15.10",
    "vivid": "15.04",
    "utopic": "14.10",
    "trusty": "14.04",
    "saucy": "13.10",
    "raring": "13.04",
    "quantal": "12.10",
    "precise": "12.04",
    "oneiric": "11.10",
    "natty": "11.04",
    "maverick": "10.10",
    "lucid": "10.04",
    "karmic": "9.10",
    "jaunty": "9.04",
    "intrepid": "8.10",
    "hardy": "8.04",
    "gutsy": "7.10",
    "feisty": "7.04",
    "edgy": "6.10",
    "dapper": "6.06",
    "breezy": "5.10",
    "hoary": "5.04",
    "warty": "4.10",
}

latest_ubuntu_version = "jammy"  # Latest LTS Version

def add_ubuntu_fetchers(only_latest = True):
    for version in ubuntu_versions:
        if only_latest and version != latest_ubuntu_version:
            continue

        fetch_repo(
            fetch_debian_repository_v2,
            (
                ubuntu_mirror,
                old_ubuntu_fallback,
                "dists/{}".format(version),
                ["amd64"],
            ),
            distro = "ubuntu@{}".format(version),
        )

debian_versions = [
    "testing",
    "sid",
    "bookworm",
    "bullseye",
    "buster",
    "stretch",
    "jessie",
    "wheezy",
    "squeeze",
    "lenny",
    "etch",
    "sarge",
    "woody",
    "potato",
    "slink",
    "hamm",
    "bo",
    "rex",
    "buzz",
]

latest_debian_version = "bookworm"

def add_debian_fetchers(only_latest = True):
    for version in debian_versions:
        if only_latest and version != latest_debian_version:
            continue

        fetch_repo(
            fetch_debian_repository_v2,
            (
                debian_mirror,
                old_debian_fallback,
                "dists/{}".format(version),
                ["amd64"],
            ),
            distro = "debian@{}".format(version),
        )

def add_kali_fetchers():
    for version in ["kali-rolling"]:
        fetch_repo(
            fetch_debian_repository_v2,
            (
                kali_mirror,
                None,
                "dists/{}".format(version),
                ["amd64"],
            ),
            distro = "kali@{}".format(version),
        )

def add_neurodebian_fetchers(only_latest = True):
    for version in ["jammy", "trusty", "noble", "mantic", "lunar", "focal", "devel", "bionic"]:
        if only_latest and version != latest_ubuntu_version:
            continue

        fetch_repo(
            fetch_debian_repository_v2,
            (
                neurodebian_mirror,
                None,
                "dists/{}".format(version),
                ["amd64"],
            ),
            distro = "ubuntu@{}".format(version),
        )

if __name__ == "__main__":
    # add_ubuntu_fetchers(only_latest = False)
    # add_debian_fetchers(only_latest = False)
    # add_kali_fetchers()
    add_neurodebian_fetchers(only_latest = False)

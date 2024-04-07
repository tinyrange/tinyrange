load("fetchers/rpm.star", "fetch_rpm_repostiory")

fedora_mirror = "https://mirror.aarnet.edu.au/pub/fedora/linux"
old_fedora_mirror = "https://dl.fedoraproject.org/pub/archive/fedora/linux"
very_old_fedora_mirror = "https://dl.fedoraproject.org/pub/archive/fedora/linux/core"

latest_fedora_version = "39"

def add_fedora_fetchers(only_latest = True):
    # Very Old Fedora Core
    for version in ["2", "3", "4", "5", "6"]:
        for arch in ["x86_64"]:
            if only_latest and version != latest_fedora_version:
                continue

            fetch_repo(
                fetch_rpm_repostiory,
                ("{}/{}/{}/os/".format(very_old_fedora_mirror, version, arch),),
                distro = "fedora@{}".format(version),
            )

    # Old Fedora Versions before Everything/Modular
    for version in [str(x) for x in range(7, 28)]:
        for arch in ["x86_64"]:
            if only_latest and version != latest_fedora_version:
                continue

            fetch_repo(
                fetch_rpm_repostiory,
                ("{}/updates/{}/{}/".format(old_fedora_mirror, version, arch),),
                distro = "fedora@{}".format(version),
            )

    # Old Fedora Versions
    for version in [str(x) for x in range(28, 37)]:
        for arch in ["x86_64"]:
            if only_latest and version != latest_fedora_version:
                continue

            fetch_repo(
                fetch_rpm_repostiory,
                ("{}/updates/{}/Everything/{}/".format(old_fedora_mirror, version, arch),),
                distro = "fedora@{}".format(version),
            )

    # Modern Supported Fedora
    for version in ["37", "38", "39", "40"]:
        for arch in ["x86_64"]:
            if only_latest and version != latest_fedora_version:
                continue

            fetch_repo(
                fetch_rpm_repostiory,
                ("{}/updates/{}/Everything/{}/".format(fedora_mirror, version, arch),),
                distro = "fedora@{}".format(version),
            )

if __name__ == "__main__":
    add_fedora_fetchers(only_latest = False)

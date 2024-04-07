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

almalinux_mirror = "https://mirror.aarnet.edu.au/pub/almalinux"
latest_almalinux_version = 9

def add_almalinux_fetchers(only_latest = True):
    for version in ["8", "9"]:
        for group in ["AppStream", "BaseOS", "HighAvailability", "NFV", "RT", "ResilientStorage", "SAP", "SAPHANA", "devel", "extras"]:
            for arch in ["x86_64"]:
                if only_latest and version != latest_almalinux_version:
                    continue

                fetch_repo(
                    fetch_rpm_repostiory,
                    ("{}/{}/{}/{}/os/".format(almalinux_mirror, version, group, arch),),
                    distro = "almalinux@{}".format(version),
                )

def add_amazonlinux_fetchers():
    fetch_repo(
        fetch_rpm_repostiory,
        ("https://cdn.amazonlinux.com/al2023/core/guids/04fa12601a9c7014b2707ed09daade3c517323c22dff283f6093dbccae52e4c2/x86_64/",),
        distro = "amazonlinux@2023",
    )

centos_stream_mirror = "https://mirror.aarnet.edu.au/pub/centos-stream"
latest_centos_stream_version = "9-stream"

def add_centos_stream_fetchers(only_latest = True):
    for version in ["9-stream"]:
        for group in ["AppStream", "BaseOS", "HighAvailability", "NFV", "RT", "ResilientStorage"]:
            for arch in ["x86_64"]:
                if only_latest and version != latest_centos_stream_version:
                    continue

                fetch_repo(
                    fetch_rpm_repostiory,
                    ("{}/{}/{}/{}/os/".format(centos_stream_mirror, version, group, arch),),
                    distro = "centos-stream@{}".format(version),
                )

if __name__ == "__main__":
    add_centos_stream_fetchers(only_latest = False)

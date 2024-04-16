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

opensuse_mirror = "https://mirror.aarnet.edu.au/pub/opensuse"
latest_opensuse_version = "15.6"

def add_opensuse_fetchers(only_latest = True):
    for version in ["15.2", "15.3", "15.4", "15.5", "15.6"]:
        for license in ["oss", "non-oss"]:
            if only_latest and version != latest_opensuse_version:
                continue

            fetch_repo(
                fetch_rpm_repostiory,
                ("{}/opensuse/distribution/leap/{}/repo/{}/".format(opensuse_mirror, version, license),),
                distro = "opensuse-leap@{}".format(version),
            )

    for license in ["oss", "non-oss"]:
        if only_latest:
            continue

        fetch_repo(
            fetch_rpm_repostiory,
            ("{}/tumbleweed/repo/{}/".format(opensuse_mirror, license),),
            distro = "opensuse-tumbleweed",
        )

oraclelinux_mirror = "https://yum.oracle.com"
latest_oraclelinux_version = "OL9"

def add_oraclelinux_fetchers(only_latest = True):
    # Older
    for version in ["OL5", "OL6", "OL7"]:
        for group in ["latest"]:
            for arch in ["x86_64"]:
                if only_latest and version != latest_opensuse_version:
                    continue

                fetch_repo(
                    fetch_rpm_repostiory,
                    ("{}/repo/OracleLinux/{}/{}/{}/".format(oraclelinux_mirror, version, group, arch),),
                    distro = "oraclelinux@{}".format(version),
                )

    # Current
    for version in ["OL9", "OL8"]:
        for group in ["baseos/latest", "appstream"]:
            for arch in ["x86_64"]:
                if only_latest and version != latest_opensuse_version:
                    continue

                fetch_repo(
                    fetch_rpm_repostiory,
                    ("{}/repo/OracleLinux/{}/{}/{}/".format(oraclelinux_mirror, version, group, arch),),
                    distro = "oraclelinux@{}".format(version),
                )

rockylinux_mirror = "https://mirror.aarnet.edu.au/pub/rocky"
latest_rockylinux_version = 9

def add_rockylinux_fetchers(only_latest = True):
    for version in ["8", "9"]:
        for group in ["AppStream", "BaseOS", "HighAvailability", "NFV", "RT", "ResilientStorage", "devel", "extras"]:
            for arch in ["x86_64"]:
                if only_latest and version != latest_rockylinux_version:
                    continue

                fetch_repo(
                    fetch_rpm_repostiory,
                    ("{}/{}/{}/{}/os/".format(rockylinux_mirror, version, group, arch),),
                    distro = "rockylinux@{}".format(version),
                )

cbl_mariner_mirror = "https://packages.microsoft.com/cbl-mariner/"

def add_cbl_mariner(only_latest = True):
    for group in ["Microsoft", "base", "cloud-native", "extended", "extras", "nvidia"]:
        fetch_repo(
            fetch_rpm_repostiory,
            ("{}/2.0/prod/{}/x86_64/".format(cbl_mariner_mirror, group),),
            distro = "cblmariner@2",
        )

if __name__ == "__main__":
    add_cbl_mariner(only_latest = False)

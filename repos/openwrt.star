load("fetchers/openwrt.star", "fetch_openwrt_repostiory")

openwrt_mirror = "https://mirror-03.infra.openwrt.org"
latest_openwrt_version = "23.05"

def add_openwrt_fetchers(only_latest = True):
    for version in ["17.01", "18.06", "19.07", "21.02", "22.03", "23.05"]:
        for arch in ["x86_64"]:
            for group in ["base", "luci", "packages", "routing", "telephony"]:
                if only_latest and version != latest_openwrt_version:
                    continue

                fetch_repo(
                    fetch_openwrt_repostiory,
                    ("{}/releases/packages-{}/{}/{}/".format(openwrt_mirror, version, arch, group),),
                    distro = "openwrt@{}".format(version),
                )

if __name__ == "__main__":
    add_openwrt_fetchers(only_latest = False)

load("fetchers/xbps.star", "fetch_xbps_repository")

mirror = "https://mirror.aarnet.edu.au/pub/voidlinux"

def add_void_fetchers():
    fetch_repo(
        fetch_xbps_repository,
        (mirror + "/current/musl", "x86_64-musl"),
        distro = "voidlinux@current-musl",
    )

    fetch_repo(
        fetch_xbps_repository,
        (mirror + "/current", "x86_64"),
        distro = "voidlinux@current",
    )

if __name__ == "__main__":
    add_void_fetchers()

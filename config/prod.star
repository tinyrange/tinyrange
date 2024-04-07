"""
Production Config with all current repo fetchers.
"""

load(
    "repos/alpine.star",
    "add_alpine_fetchers",
    "add_postmarketos_fetchers",
    "add_wolfi_fetchers",
)
load(
    "repos/arch.star",
    "add_arch_fetchers",
    "add_aur_fetchers",
    "add_bioarch_fetchers",
)
load(
    "repos/debian.star",
    "add_debian_fetchers",
    "add_kali_fetchers",
    "add_ubuntu_fetchers",
)
load(
    "repos/rpm.star",
    "add_almalinux_fetchers",
    "add_amazonlinux_fetchers",
    "add_centos_stream_fetchers",
    "add_fedora_fetchers",
)
load(
    "repos/xbps.star",
    "add_void_fetchers",
)

# Alpine Linux
add_alpine_fetchers(only_latest = False)
add_wolfi_fetchers()
add_postmarketos_fetchers(only_latest = False)

# Arch Linux
add_arch_fetchers()
add_bioarch_fetchers()
add_aur_fetchers()

# Debian
add_ubuntu_fetchers(only_latest = False)
add_debian_fetchers(only_latest = False)
add_kali_fetchers()

# Void Linux
add_void_fetchers()

# RPM
add_fedora_fetchers(only_latest = False)
add_almalinux_fetchers(only_latest = False)
add_amazonlinux_fetchers()
add_centos_stream_fetchers(only_latest = False)

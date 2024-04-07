load("repos/alpine.star", "add_alpine_fetchers", "add_postmarketos_fetchers", "add_wolfi_fetchers")
load("repos/arch.star", "add_arch_fetchers", "add_aur_fetchers", "add_bioarch_fetchers")
load("repos/debian.star", "add_debian_fetchers", "add_kali_fetchers", "add_ubuntu_fetchers")

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

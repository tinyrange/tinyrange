load("repos/alpine.star", "add_alpine_fetchers", "add_postmarketos_fetchers", "add_wolfi_fetchers")

# Alpine Linux
add_alpine_fetchers(only_latest = False)
add_wolfi_fetchers()
add_postmarketos_fetchers(only_latest = False)

"""
Experimental Fetcher for Nix

Designed to support Nixpkgs

# Requirements

- Needs two files in the local filesystem.
  - nix_drv.tar.gz: A compressed nix store of only derivations. Something like this can be made by evaluating the top level of nixpkgs in a nix repl.
  - local/nixpkgs.json: The metadata from nixpkgs. `nix-env --json -f "<nixpkgs>" --arg config "import <nixpkgs/pkgs/top-level/packages-config.nix>" -qa --meta > nixpkgs.json` is how I generate this.

# Plan

- I'll add support for reading the narinfo and nar archives so these packages can be downloaded.
"""

def fetch_nix_repostitory(ctx, archive_path, metadata_path):
    f = open(archive_path)

    ents = f.read_archive(archive_path)

    for ent in ents:
        if not ent.name.endswith(".drv"):
            continue

        name = ent.name.removeprefix("./").removesuffix(".drv")
        drv_hash, drv_name = name.split("-", 1)

        pkg = ctx.add_package(ctx.name(
            name = drv_name,
            version = drv_hash,
        ))

        contents_str = ent.read()

        if len(contents_str) == 0:
            continue

        contents = parse_nix_derivation(contents_str)

        for input in contents["inputDrvs"]:
            name = input.removeprefix("/nix/store/").removesuffix(".drv")
            input_hash, input_name = name.split("-", 1)
            pkg.add_dependency(ctx.name(
                name = input_name,
                version = input_hash,
            ))

    metadata = json.decode(open(metadata_path).read())

    for name in metadata:
        meta_pkg = metadata[name]
        pkg = ctx.add_package(ctx.name(
            name = meta_pkg["pname"],
            version = meta_pkg["version"],
        ))
        pkg.add_dependency(ctx.name(name = meta_pkg["name"]))

fetch_repo(fetch_nix_repostitory, ("local/nix_drv.tar.gz", "local/nixpkgs.json"), distro = "nix")

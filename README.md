# Package Metadata Database

`pkg2` is a kitchen sink for pulling package system metadata. Fetchers are implemented to create indexes of package repositories and `pkg2` supports resolving dependencies to satisfy installation plans. All of this is exposed though a web interface and a scripting interface.

It includes fetchers for...

- Alpine Linux
- Debian/Ubuntu/etc...
- Fedora/RPM/etc...
- Void Linux
- Arch Linux
- Arch Linux AUR
- EasyBuild
- CRAN
- Cargo
- Pypi
- Spack
- OpenWRT
- NeuroContainers from the NeuroDesk project (https://www.neurodesk.org/).

All fetchers are declaratively configured using Starlark.

The scripting interface has powerful parallel fetching and caching functionality with support for...

- Downloading files from web servers with caching.
- Extracting and decompressing any file-shaped object (supports `.tar`, `.tar.gz`, `.tar.xz`, `.tar.bz2`, `.tar.zst`, `.ar`).
- Parsing various file formats (JSON, XML, YAML, Nix Derivation, Plist)
- Downloading Git repositories and accessing arbitrary branches, tags, and commits.
- Evaluating Starlark scripts with configurable functions exposed.
- Transpiling simple Python programs into Starlark and evaluating them.
- Evaluating Jinja2 templates (partial implementation).
- Evaluating Shell scripts with configurable filesystems and command implementations.
- Evaluating Makefiles and getting information for individual rules.

## Building

`pkg2` is a regular Go module. We don't have stable versions so please work from a checkout of this repository.

```
git clone https://github.com/tinyrange/pkg2.git
```

You can start a copy of the public instance with the following command.

```
go run . config/public.star
```

If you feel like downloading the metadata of around 4-5 million packages then you can load most of the fetchers with.

```
go run . config/all.star
```

## Contributing

Pull requests are welcome and any issues reported are highly appreciated.
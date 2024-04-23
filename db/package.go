package db

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"

	"go.starlark.net/starlark"
)

func versionGreaterThan(a, b string) bool {
	return true
}

func versionLessThan(a, b string) bool {
	return true
}

type CPUArchitecture string

const (
	ArchInvalid CPUArchitecture = ""
	ArchAArch64 CPUArchitecture = "aarch64"
	ArchArmHF   CPUArchitecture = "armhf"
	ArchArmV7   CPUArchitecture = "armv7"
	ArchMips64  CPUArchitecture = "mips64"
	ArchPPC64LE CPUArchitecture = "ppc64le"
	ArchRiscV64 CPUArchitecture = "riscv64"
	ArchS390X   CPUArchitecture = "s390x"
	ArchI386    CPUArchitecture = "i386"
	ArchI586    CPUArchitecture = "i586"
	ArchI686    CPUArchitecture = "i686"
	ArchX86_64  CPUArchitecture = "x86_64"
	ArchAny     CPUArchitecture = "any"
	ArchSource  CPUArchitecture = "src"
)

type PackageName struct {
	Distribution string
	Name         string
	Version      string
	Architecture string
}

func (name PackageName) Matches(query PackageName) bool {
	if query.Distribution != "" {
		if query.Distribution != name.Distribution {
			return false
		}
	}

	if query.Architecture != "" {
		if query.Architecture != name.Architecture {
			return false
		}
	}

	if query.Name != "" {
		if query.Name != name.Name {
			return false
		}
	}

	if query.Version != "" {
		if strings.HasPrefix(query.Version, "<") {
			if !versionLessThan(name.Version, query.Version) {
				return false
			}
		} else if strings.HasPrefix(query.Version, ">") {
			if !versionGreaterThan(name.Version, query.Version) {
				return false
			}
		} else if query.Version != name.Version {
			return false
		}
	}

	return true
}

func (name PackageName) Path() []string {
	distributionName, distributionVersion, ok := strings.Cut(name.Distribution, "@")
	if !ok {
		distributionVersion = "latest"
	}

	return []string{name.Name, name.Version, distributionName, distributionVersion, name.Architecture}
}

func (name PackageName) String() string {
	return fmt.Sprintf("%s/%s@%s:%s", name.Distribution, name.Name, name.Version, name.Architecture)
}

func (name PackageName) ShortName() string {
	return fmt.Sprintf("@/%s", name.Name)
}

// Attr implements starlark.HasAttrs.
func (n PackageName) Attr(name string) (starlark.Value, error) {
	if name == "distribution" {
		return starlark.String(n.Distribution), nil
	} else if name == "name" {
		return starlark.String(n.Name), nil
	} else if name == "version" {
		return starlark.String(n.Version), nil
	} else if name == "architecture" {
		return starlark.String(n.Architecture), nil
	} else if name == "set_version" {
		return starlark.NewBuiltin("PackageName.set_version", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				version string
			)

			if err := starlark.UnpackArgs("PackageName.set_version", args, kwargs,
				"version", &version,
			); err != nil {
				return starlark.None, err
			}

			return NewPackageName(n.Distribution, n.Name, version, n.Architecture)
		}), nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (name PackageName) AttrNames() []string {
	return []string{"distribution", "name", "version", "architecture", "set_version"}
}

func (name PackageName) UrlParams() string {
	values := url.Values{}
	values.Add("distribution", name.Distribution)
	values.Add("name", name.Name)
	values.Add("version", name.Version)
	values.Add("architecture", name.Architecture)
	return values.Encode()
}

func (PackageName) Type() string          { return "PackageName" }
func (PackageName) Hash() (uint32, error) { return 0, fmt.Errorf("PackageName is not hashable") }
func (PackageName) Truth() starlark.Bool  { return starlark.True }
func (PackageName) Freeze()               {}

var (
	_ starlark.Value    = PackageName{}
	_ starlark.HasAttrs = PackageName{}
)

func NewPackageName(distribution, name, version, architecture string) (PackageName, error) {
	return PackageName{
		Distribution: distribution,
		Name:         name,
		Version:      version,
		Architecture: architecture,
	}, nil
}

func ParsePackageName(s string) (PackageName, error) {
	if strings.Contains(s, "/") {
		distribution, name, _ := strings.Cut(s, "/")
		return NewPackageName(distribution, name, "", "")
	} else {
		return NewPackageName("", s, "", "")
	}
}

type BuildScript struct {
	Name string
	Args []string
}

type PackageMetadataVersion int

const (
	PackageMetadataVersionCurrent PackageMetadataVersion = 1
)

type Package struct {
	MetadataVersion PackageMetadataVersion
	Name            PackageName
	Description     string
	License         string
	Size            int
	InstalledSize   int
	DownloadUrls    []string
	Metadata        map[string]string
	Depends         [][]PackageName
	Aliases         []PackageName
	BuildScripts    []BuildScript
	Builders        []*Builder
}

func (pkg *Package) Encode(w io.Writer) error {
	enc := json.NewEncoder(w)

	return enc.Encode(pkg)
}

func (pkg *Package) Decode(r io.Reader) error {
	enc := json.NewDecoder(r)

	return enc.Decode(pkg)
}

func (pkg *Package) Id() string {
	return pkg.Name.String()
}

func (pkg *Package) Matches(query PackageName) bool {
	ok := pkg.Name.Matches(query)
	if ok {
		return true
	}

	for _, alias := range pkg.Aliases {
		if ok = alias.Matches(query); ok {
			return true
		}
	}

	return false
}

// Attr implements starlark.HasAttrs.
func (pkg *Package) Attr(name string) (starlark.Value, error) {
	if name == "set_description" {
		return starlark.NewBuiltin("Package.set_description", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				description string
			)

			if err := starlark.UnpackArgs("Package.set_description", args, kwargs,
				"description", &description,
			); err != nil {
				return starlark.None, err
			}

			pkg.Description = description

			return starlark.None, nil
		}), nil
	} else if name == "set_license" {
		return starlark.NewBuiltin("Package.set_license", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				license string
			)

			if err := starlark.UnpackArgs("Package.set_license", args, kwargs,
				"license", &license,
			); err != nil {
				return starlark.None, err
			}

			pkg.License = license

			return starlark.None, nil
		}), nil
	} else if name == "set_size" {
		return starlark.NewBuiltin("Package.set_size", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				size int
			)

			if err := starlark.UnpackArgs("Package.set_size", args, kwargs,
				"size", &size,
			); err != nil {
				return starlark.None, err
			}

			pkg.Size = size

			return starlark.None, nil
		}), nil
	} else if name == "set_installed_size" {
		return starlark.NewBuiltin("Package.set_installed_size", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				size int
			)

			if err := starlark.UnpackArgs("Package.set_installed_size", args, kwargs,
				"size", &size,
			); err != nil {
				return starlark.None, err
			}

			pkg.InstalledSize = size

			return starlark.None, nil
		}), nil
	} else if name == "add_source" {
		return starlark.NewBuiltin("Package.add_source", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				url string
			)

			if err := starlark.UnpackArgs("Package.add_source", args, kwargs,
				"url", &url,
			); err != nil {
				return starlark.None, err
			}

			pkg.DownloadUrls = append(pkg.DownloadUrls, url)

			return starlark.None, nil
		}), nil
	} else if name == "add_metadata" {
		return starlark.NewBuiltin("Package.add_metadata", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				key   string
				value string
			)

			if err := starlark.UnpackArgs("Package.add_metadata", args, kwargs,
				"key", &key,
				"value", &value,
			); err != nil {
				return starlark.None, err
			}

			if value != "" {
				pkg.Metadata[key] = value
			}

			return starlark.None, nil
		}), nil
	} else if name == "add_dependency" {
		return starlark.NewBuiltin("Package.add_dependency", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				name starlark.Value
				kind string
			)

			if err := starlark.UnpackArgs("Package.add_alias", args, kwargs,
				"name", &name,
				"kind?", &kind,
			); err != nil {
				return starlark.None, err
			}

			if pkgName, ok := name.(PackageName); ok {
				pkg.Depends = append(pkg.Depends, []PackageName{pkgName})

				return starlark.None, nil
			} else if names, ok := name.(*starlark.List); ok {
				var options []PackageName

				var err error

				names.Elements(func(v starlark.Value) bool {
					pkgName, ok := v.(PackageName)
					if ok {
						options = append(options, pkgName)
						return true
					} else {
						err = fmt.Errorf("expected PackageName got %s", name.Type())
						return false
					}
				})
				if err != nil {
					return starlark.None, err
				}

				pkg.Depends = append(pkg.Depends, options)

				return starlark.None, nil
			} else {
				return starlark.None, fmt.Errorf("unhandled argument type: %T", name)
			}
		}), nil
	} else if name == "add_alias" {
		return starlark.NewBuiltin("Package.add_alias", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				name PackageName
				kind string
			)

			if err := starlark.UnpackArgs("Package.add_alias", args, kwargs,
				"name", &name,
				"kind?", &kind,
			); err != nil {
				return starlark.None, err
			}

			pkg.Aliases = append(pkg.Aliases, name)

			return starlark.None, nil
		}), nil
	} else if name == "add_build_script" {
		return starlark.NewBuiltin("Package.add_build_script", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				name  string
				fArgs starlark.Tuple
			)

			if err := starlark.UnpackArgs("Package.add_build_script", args, kwargs,
				"name", &name,
				"fArgs", &fArgs,
			); err != nil {
				return starlark.None, err
			}

			var scriptArgs []string
			for _, arg := range fArgs {
				str, ok := starlark.AsString(arg)
				if !ok {
					return starlark.None, fmt.Errorf("failed to convert to string: %s", arg.Type())
				}

				scriptArgs = append(scriptArgs, str)
			}

			pkg.BuildScripts = append(pkg.BuildScripts, BuildScript{
				Name: name,
				Args: scriptArgs,
			})

			return starlark.None, nil
		}), nil
	} else if name == "add_builder" {
		return starlark.NewBuiltin("Package.add_builder", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				builder *Builder
			)

			if err := starlark.UnpackArgs("Package.add_builder", args, kwargs,
				"builder", &builder,
			); err != nil {
				return starlark.None, err
			}

			pkg.Builders = append(pkg.Builders, builder)

			return starlark.None, nil
		}), nil
	} else if name == "name" {
		return starlark.String(pkg.Name.Name), nil
	} else if name == "version" {
		return starlark.String(pkg.Name.Version), nil
	} else if name == "arch" {
		return starlark.String(pkg.Name.Architecture), nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (*Package) AttrNames() []string {
	return []string{
		"set_description",
		"set_license",
		"set_size",
		"set_installed_size",
		"add_source",
		"add_metadata",
		"add_dependency",
		"add_alias",
		"add_build_script",
		"add_builder",
		"name",
		"version",
		"arch",
	}
}

func (*Package) String() string        { return "Package" }
func (*Package) Type() string          { return "Package" }
func (*Package) Hash() (uint32, error) { return 0, fmt.Errorf("Package is not hashable") }
func (*Package) Truth() starlark.Bool  { return starlark.True }
func (*Package) Freeze()               {}

var (
	_ starlark.Value    = &Package{}
	_ starlark.HasAttrs = &Package{}
)

func NewPackage() *Package {
	return &Package{
		MetadataVersion: PackageMetadataVersionCurrent,
		Metadata:        make(map[string]string),
	}
}

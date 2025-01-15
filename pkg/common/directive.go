package common

import (
	"fmt"
	"strings"

	"github.com/tinyrange/tinyrange/pkg/config"
	"github.com/tinyrange/tinyrange/pkg/hash"
	"go.starlark.net/starlark"
)

func init() {
	hash.RegisterType(DirectiveRunCommand{})
	hash.RegisterType(DirectiveEnvironment{})
	hash.RegisterType(DirectiveList{})
	hash.RegisterType(DirectiveAddPackage{})
	hash.RegisterType(DirectiveInteraction{})
	hash.RegisterType(DirectiveDefaultInteractive{})
	hash.RegisterType(DirectiveMountHostDirectory{})
	hash.RegisterType(DirectiveKernel{})
	hash.RegisterType(DirectiveAddInitScript{})
}

type Directive interface {
	Tag() string
	AsFragments(ctx BuildContext1, special SpecialDirectiveHandlers) ([]config.Fragment, error)
}

type DirectiveRunCommand struct {
	Command string
}

// SerializableType implements Directive.
func (d DirectiveRunCommand) SerializableType() string { return "DirectiveRunCommand" }

// AsFragments implements Directive.
func (d DirectiveRunCommand) AsFragments(ctx BuildContext1, special SpecialDirectiveHandlers) ([]config.Fragment, error) {
	return []config.Fragment{
		{RunCommand: &config.RunCommandFragment{Command: string(d.Command)}},
	}, nil
}

// Tag implements Directive.
func (d DirectiveRunCommand) Tag() string {
	return fmt.Sprintf("RunCommand_%s", strings.ReplaceAll(string(d.Command), " ", "_"))
}

type DirectiveStartServiceCommand struct {
	Command string
}

// SerializableType implements Directive.
func (d DirectiveStartServiceCommand) SerializableType() string {
	return "DirectiveStartServiceCommand"
}

// AsFragments implements Directive.
func (d DirectiveStartServiceCommand) AsFragments(ctx BuildContext1, special SpecialDirectiveHandlers) ([]config.Fragment, error) {
	return []config.Fragment{
		{StartServiceCommand: &config.StartServiceCommandFragment{Command: string(d.Command)}},
	}, nil
}

// Tag implements Directive.
func (d DirectiveStartServiceCommand) Tag() string {
	return fmt.Sprintf("StartServiceCommand_%s", strings.ReplaceAll(string(d.Command), " ", "_"))
}

type DirectiveAddFile struct {
	Filename   string
	Definition BuildDefinition1
	Contents   []byte
	Executable bool
}

// SerializableType implements Directive.
func (d DirectiveAddFile) SerializableType() string { return "DirectiveAddFile" }

// AsFragments implements Directive.
func (d DirectiveAddFile) AsFragments(ctx BuildContext1, special SpecialDirectiveHandlers) ([]config.Fragment, error) {
	if d.Definition != nil {
		art, err := ctx.BuildChild(d.Definition)
		if err != nil {
			return nil, err
		}

		res, err := art.Default()
		if err != nil {
			return nil, err
		}

		digest, err := ctx.DigestFromFile(res)
		if err != nil {
			return nil, err
		}

		filename, err := ctx.FilenameFromDigest(digest)
		if err != nil {
			return nil, err
		}

		return []config.Fragment{
			{LocalFile: &config.LocalFileFragment{
				GuestFilename: d.Filename,
				HostFilename:  filename,
				Executable:    d.Executable,
			}},
		}, nil
	} else {
		return []config.Fragment{
			{FileContents: &config.FileContentsFragment{
				GuestFilename: d.Filename,
				Contents:      d.Contents,
				Executable:    d.Executable,
			}},
		}, nil
	}
}

// Tag implements Directive.
func (d DirectiveAddFile) Tag() string {
	if d.Definition != nil {
		return fmt.Sprintf("AddFile_%s_%s_%+v", d.Filename, d.Definition.Tag(), d.Executable)
	} else {
		sum := hash.GetSha256Hash(d.Contents)

		return fmt.Sprintf("AddFile_%s_%s_%+v", d.Filename, sum, d.Executable)
	}
}

type DirectiveLocalFile struct {
	Filename     string
	HostFilename string
}

// AsFragments implements Directive.
func (d DirectiveLocalFile) AsFragments(ctx BuildContext1, special SpecialDirectiveHandlers) ([]config.Fragment, error) {
	return []config.Fragment{
		{LocalFile: &config.LocalFileFragment{
			HostFilename:  d.HostFilename,
			GuestFilename: d.Filename,
		}},
	}, nil
}

// SerializableType implements Directive.
func (d DirectiveLocalFile) SerializableType() string { return "DirectiveLocalFile" }

// Tag implements Directive.
func (d DirectiveLocalFile) Tag() string {
	return fmt.Sprintf("LocalFile_%s_%s", d.Filename, d.HostFilename)
}

type DirectiveArchive struct {
	Definition BuildDefinition1
	Target     string
}

// SerializableType implements Directive.
func (d DirectiveArchive) SerializableType() string { return "DirectiveArchive" }

// AsFragments implements Directive.
func (d DirectiveArchive) AsFragments(ctx BuildContext1, special SpecialDirectiveHandlers) ([]config.Fragment, error) {
	art, err := ctx.BuildChild(d.Definition)
	if err != nil {
		return nil, err
	}

	res, err := art.Default()
	if err != nil {
		return nil, err
	}

	digest, err := ctx.DigestFromFile(res)
	if err != nil {
		return nil, err
	}

	filename, err := ctx.FilenameFromDigest(digest)
	if err != nil {
		return nil, err
	}

	return []config.Fragment{
		{Archive: &config.ArchiveFragment{
			HostFilename: filename,
			Target:       d.Target,
		}},
	}, nil
}

// Tag implements Directive.
func (d DirectiveArchive) Tag() string {
	return fmt.Sprintf("DirArchive_%s_%s", d.Definition.Tag(), d.Target)
}

type DirectiveExportPort struct {
	Name string
	Port int
}

// SerializableType implements Directive.
func (d DirectiveExportPort) SerializableType() string { return "DirectiveExportPort" }

// AsFragments implements Directive.
func (d DirectiveExportPort) AsFragments(ctx BuildContext1, special SpecialDirectiveHandlers) ([]config.Fragment, error) {
	return []config.Fragment{
		{ExportPort: &config.ExportPortFragment{Name: d.Name, Port: d.Port}},
	}, nil
}

// Tag implements Directive.
func (d DirectiveExportPort) Tag() string {
	return fmt.Sprintf("DirPort_%s_%d", d.Name, d.Port)
}

type DirectiveEnvironment struct {
	Variables []string
}

// SerializableType implements Directive.
func (d DirectiveEnvironment) SerializableType() string { return "DirectiveEnvironment" }

// AsFragments implements Directive.
func (d DirectiveEnvironment) AsFragments(ctx BuildContext1, special SpecialDirectiveHandlers) ([]config.Fragment, error) {
	return []config.Fragment{
		{Environment: &config.EnvironmentFragment{Variables: d.Variables}},
	}, nil
}

// Tag implements Directive.
func (d DirectiveEnvironment) Tag() string {
	return fmt.Sprintf("DirEnvironment_%+v", d.Variables)
}

type DirectiveBuiltin struct {
	Name          string
	Architecture  string
	GuestFilename string
}

// SerializableType implements Directive.
func (d DirectiveBuiltin) SerializableType() string { return "DirectiveFragment" }

// AsFragments implements Directive.
func (d DirectiveBuiltin) AsFragments(ctx BuildContext1, special SpecialDirectiveHandlers) ([]config.Fragment, error) {
	return []config.Fragment{
		{Builtin: &config.BuiltinFragment{Name: d.Name, Architecture: config.CPUArchitecture(d.Architecture), GuestFilename: d.GuestFilename}},
	}, nil
}

// Tag implements Directive.
func (d DirectiveBuiltin) Tag() string {
	return fmt.Sprintf("BuiltinFrag_%s_%s", d.Name, d.GuestFilename)
}

type DirectiveList struct {
	Items []Directive
}

// AsFragments implements Directive.
func (d DirectiveList) AsFragments(ctx BuildContext1, special SpecialDirectiveHandlers) ([]config.Fragment, error) {
	var ret []config.Fragment

	for _, dir := range d.Items {
		frags, err := dir.AsFragments(ctx, special)
		if err != nil {
			return nil, err
		}

		ret = append(ret, frags...)
	}

	return ret, nil
}

// SerializableType implements Directive.
func (d DirectiveList) SerializableType() string { return "DirectiveList" }

// Tag implements Directive.
func (d DirectiveList) Tag() string {
	var ret []string

	for _, dir := range d.Items {
		ret = append(ret, dir.Tag())
	}

	return strings.Join(ret, "_")
}

type DirectiveAddPackage struct {
	Name PackageQuery
}

// AsFragments implements Directive.
func (d DirectiveAddPackage) AsFragments(ctx BuildContext1, special SpecialDirectiveHandlers) ([]config.Fragment, error) {
	return nil, fmt.Errorf("DirectiveAddPackage cannot be represented as a fragment")
}

// SerializableType implements Directive.
func (d DirectiveAddPackage) SerializableType() string { return "DirectiveAddPackage" }

// Tag implements Directive.
func (d DirectiveAddPackage) Tag() string {
	return d.Name.String()
}

type DirectiveInteraction struct {
	Interaction string
}

// AsFragments implements Directive.
func (d DirectiveInteraction) AsFragments(ctx BuildContext1, special SpecialDirectiveHandlers) ([]config.Fragment, error) {
	return nil, fmt.Errorf("DirectiveInteraction cannot be represented as a fragment")
}

// SerializableType implements Directive.
func (d DirectiveInteraction) SerializableType() string { return "DirectiveInteraction" }

// Tag implements Directive.
func (d DirectiveInteraction) Tag() string {
	return fmt.Sprintf("DirectiveInteraction_%s", d.Interaction)
}

type DirectiveDefaultInteractive struct {
	InteractiveCommand []string
}

// AsFragments implements Directive.
func (d DirectiveDefaultInteractive) AsFragments(ctx BuildContext1, special SpecialDirectiveHandlers) ([]config.Fragment, error) {
	if special.DefaultInteractive != nil {
		if err := special.DefaultInteractive(d); err != nil {
			return nil, nil
		}

		return nil, nil
	}
	return []config.Fragment{
		{DefaultInteractive: &config.DefaultInteractiveFragment{Args: d.InteractiveCommand}},
	}, nil
}

// SerializableType implements Directive.
func (d DirectiveDefaultInteractive) SerializableType() string { return "DirectiveDefaultInteractive" }

// Tag implements Directive.
func (d DirectiveDefaultInteractive) Tag() string {
	return fmt.Sprintf("DirectiveDefaultInteractive_%+v", d.InteractiveCommand)
}

type DirectiveMountHostDirectory struct {
	HostDirectory string
	Writable      bool
}

// AsFragments implements Directive.
func (d DirectiveMountHostDirectory) AsFragments(ctx BuildContext1, special SpecialDirectiveHandlers) ([]config.Fragment, error) {
	return []config.Fragment{
		{MountHostDirectory: &config.MountHostDirectoryFragment{HostDirectory: d.HostDirectory, Writable: d.Writable}},
	}, nil
}

// SerializableType implements Directive.
func (d DirectiveMountHostDirectory) SerializableType() string { return "DirectiveMountHostDirectory" }

// Tag implements Directive.
func (d DirectiveMountHostDirectory) Tag() string {
	return fmt.Sprintf("DirectiveMountHostDirectory_%s", d.HostDirectory)
}

type DirectiveKernel struct {
	Kernel    BuildDefinition1
	Initramfs BuildDefinition1
}

// AsFragments implements Directive.
func (d DirectiveKernel) AsFragments(ctx BuildContext1, special SpecialDirectiveHandlers) ([]config.Fragment, error) {
	kernelArt, err := ctx.BuildChild(d.Kernel)
	if err != nil {
		return nil, err
	}

	kernelRes, err := kernelArt.Default()
	if err != nil {
		return nil, err
	}

	kernelDigest, err := ctx.DigestFromFile(kernelRes)
	if err != nil {
		return nil, err
	}

	kernelFilename, err := ctx.FilenameFromDigest(kernelDigest)
	if err != nil {
		return nil, err
	}

	initramfsArt, err := ctx.BuildChild(d.Initramfs)
	if err != nil {
		return nil, err
	}

	initramfsRes, err := initramfsArt.Default()
	if err != nil {
		return nil, err
	}

	initramfsDigest, err := ctx.DigestFromFile(initramfsRes)
	if err != nil {
		return nil, err
	}

	initramfsFilename, err := ctx.FilenameFromDigest(initramfsDigest)
	if err != nil {
		return nil, err
	}

	return []config.Fragment{
		{Kernel: &config.KernelFragment{
			KernelFilename:    kernelFilename,
			InitramfsFilename: initramfsFilename,
		}},
	}, nil
}

// SerializableType implements Directive.
func (d DirectiveKernel) SerializableType() string { return "DirectiveKernel" }

// Tag implements Directive.
func (d DirectiveKernel) Tag() string {
	return fmt.Sprintf("DirectiveKernel_%s_%s", d.Kernel.Tag(), d.Initramfs.Tag())
}

type DirectiveAddInitScript struct {
	GuestFilename string
}

// AsFragments implements Directive.
func (d DirectiveAddInitScript) AsFragments(ctx BuildContext1, special SpecialDirectiveHandlers) ([]config.Fragment, error) {
	return []config.Fragment{
		{AddInitScript: &config.AddInitScriptFragment{GuestFilename: d.GuestFilename}},
	}, nil
}

// SerializableType implements Directive.
func (d DirectiveAddInitScript) SerializableType() string { return "DirectiveAddInitScript" }

// Tag implements Directive.
func (d DirectiveAddInitScript) Tag() string {
	return fmt.Sprintf("DirectiveAddInitScript_%s", d.GuestFilename)
}

var (
	_ Directive = DirectiveRunCommand{}
	_ Directive = DirectiveStartServiceCommand{}
	_ Directive = DirectiveAddFile{}
	_ Directive = DirectiveLocalFile{}
	_ Directive = DirectiveArchive{}
	_ Directive = DirectiveExportPort{}
	_ Directive = DirectiveEnvironment{}
	_ Directive = DirectiveBuiltin{}
	_ Directive = DirectiveList{}
	_ Directive = DirectiveAddPackage{}
	_ Directive = DirectiveInteraction{}
	_ Directive = DirectiveDefaultInteractive{}
	_ Directive = DirectiveMountHostDirectory{}
	_ Directive = DirectiveKernel{}
	_ Directive = DirectiveAddInitScript{}
)

type StarDirective struct {
	Directive Directive
}

func (d *StarDirective) String() string      { return d.Directive.Tag() }
func (d *StarDirective) Type() string        { return fmt.Sprintf("%T", d.Directive) }
func (*StarDirective) Hash() (uint32, error) { return 0, fmt.Errorf("Directive is not hashable") }
func (*StarDirective) Truth() starlark.Bool  { return starlark.True }
func (*StarDirective) Freeze()               {}

var (
	_ starlark.Value = &StarDirective{}
)

type SpecialDirectiveHandlers struct {
	RunCommand          func(dir DirectiveRunCommand) error
	StartServiceCommand func(dir DirectiveStartServiceCommand) error
	AddInitScript       func(dir DirectiveAddInitScript) error
	AddPackage          func(dir DirectiveAddPackage) error
	Environment         func(dir DirectiveEnvironment) error
	Interaction         func(dir DirectiveInteraction) error
	DefaultInteractive  func(dir DirectiveDefaultInteractive) error
	MountHostDirectory  func(dir DirectiveMountHostDirectory) error
	Kernel              func(dir DirectiveKernel) error
}

func FlattenDirectives(directives []Directive, handlers SpecialDirectiveHandlers) ([]Directive, error) {
	var ret []Directive

	var recurse func(directives []Directive) error

	recurse = func(directives []Directive) error {
		for _, dir := range directives {
			switch dir := dir.(type) {
			case DirectiveRunCommand:
				if handlers.RunCommand != nil {
					if err := handlers.RunCommand(dir); err != nil {
						return err
					}
				} else {
					ret = append(ret, dir)
				}
			case DirectiveStartServiceCommand:
				if handlers.StartServiceCommand != nil {
					if err := handlers.StartServiceCommand(dir); err != nil {
						return err
					}
				} else {
					ret = append(ret, dir)
				}
			case DirectiveAddInitScript:
				if handlers.AddInitScript != nil {
					if err := handlers.AddInitScript(dir); err != nil {
						return err
					}
				} else {
					ret = append(ret, dir)
				}
			case DirectiveAddPackage:
				if handlers.AddPackage != nil {
					if err := handlers.AddPackage(dir); err != nil {
						return err
					}
				} else {
					ret = append(ret, dir)
				}
			case DirectiveEnvironment:
				if handlers.Environment != nil {
					if err := handlers.Environment(dir); err != nil {
						return err
					}
				} else {
					ret = append(ret, dir)
				}
			case DirectiveInteraction:
				if handlers.Interaction != nil {
					if err := handlers.Interaction(dir); err != nil {
						return err
					}
				} else {
					ret = append(ret, dir)
				}
			case DirectiveDefaultInteractive:
				if handlers.DefaultInteractive != nil {
					if err := handlers.DefaultInteractive(dir); err != nil {
						return err
					}
				} else {
					ret = append(ret, dir)
				}
			case DirectiveMountHostDirectory:
				if handlers.MountHostDirectory != nil {
					if err := handlers.MountHostDirectory(dir); err != nil {
						return err
					}
				} else {
					ret = append(ret, dir)
				}
			case DirectiveKernel:
				if handlers.Kernel != nil {
					if err := handlers.Kernel(dir); err != nil {
						return err
					}
				} else {
					ret = append(ret, dir)
				}
			case DirectiveList:
				if err := recurse(dir.Items); err != nil {
					return err
				}
			default:
				ret = append(ret, dir)
			}
		}

		return nil
	}

	if err := recurse(directives); err != nil {
		return nil, err
	}

	return ret, nil
}

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
	hash.SerializableValue

	Dependencies() ([]BuildDefinition, error)
	AsFragments(ctx BuildContext, special SpecialDirectiveHandlers) ([]config.Fragment, error)
}

type DirectiveRunCommand struct {
	Command string
}

// SerializableType implements Directive.
func (d DirectiveRunCommand) SerializableType() string { return "DirectiveRunCommand" }

// Dependencies implements Directive.
func (d DirectiveRunCommand) Dependencies() ([]BuildDefinition, error) { return nil, nil }

// AsFragments implements Directive.
func (d DirectiveRunCommand) AsFragments(ctx BuildContext, special SpecialDirectiveHandlers) ([]config.Fragment, error) {
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

// Dependencies implements Directive.
func (d DirectiveStartServiceCommand) Dependencies() ([]BuildDefinition, error) { return nil, nil }

// AsFragments implements Directive.
func (d DirectiveStartServiceCommand) AsFragments(ctx BuildContext, special SpecialDirectiveHandlers) ([]config.Fragment, error) {
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
	Definition BuildDefinition
	Contents   []byte
	Executable bool
}

// SerializableType implements Directive.
func (d DirectiveAddFile) SerializableType() string { return "DirectiveAddFile" }

// Dependencies implements Directive.
func (d DirectiveAddFile) Dependencies() ([]BuildDefinition, error) {
	if d.Definition != nil {
		return []BuildDefinition{d.Definition}, nil
	} else {
		return nil, nil
	}
}

// AsFragments implements Directive.
func (d DirectiveAddFile) AsFragments(ctx BuildContext, special SpecialDirectiveHandlers) ([]config.Fragment, error) {
	if d.Definition != nil {
		art, err := ctx.BuildChild(d.Definition)
		if err != nil {
			return nil, err
		}

		res, err := art.Default()
		if err != nil {
			return nil, err
		}

		filename, err := ctx.HostFilenameFromFile(res)
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

type DirectiveLocalFile struct {
	Filename     string
	HostFilename string
}

// AsFragments implements Directive.
func (d DirectiveLocalFile) AsFragments(ctx BuildContext, special SpecialDirectiveHandlers) ([]config.Fragment, error) {
	return []config.Fragment{
		{LocalFile: &config.LocalFileFragment{
			HostFilename:  d.HostFilename,
			GuestFilename: d.Filename,
		}},
	}, nil
}

// Dependencies implements Directive.
func (d DirectiveLocalFile) Dependencies() ([]BuildDefinition, error) { return nil, nil }

// SerializableType implements Directive.
func (d DirectiveLocalFile) SerializableType() string { return "DirectiveLocalFile" }

// Tag implements Directive.
func (d DirectiveLocalFile) Tag() string {
	return fmt.Sprintf("LocalFile_%s_%s", d.Filename, d.HostFilename)
}

type DirectiveArchive struct {
	Definition BuildDefinition
	Target     string
}

// SerializableType implements Directive.
func (d DirectiveArchive) SerializableType() string { return "DirectiveArchive" }

// Dependencies implements Directive.
func (d DirectiveArchive) Dependencies() ([]BuildDefinition, error) {
	return []BuildDefinition{d.Definition}, nil
}

// AsFragments implements Directive.
func (d DirectiveArchive) AsFragments(ctx BuildContext, special SpecialDirectiveHandlers) ([]config.Fragment, error) {
	art, err := ctx.BuildChild(d.Definition)
	if err != nil {
		return nil, err
	}

	res, err := art.Default()
	if err != nil {
		return nil, err
	}

	filename, err := ctx.HostFilenameFromFile(res)
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

type DirectiveExportPort struct {
	Name string
	Port int
}

// SerializableType implements Directive.
func (d DirectiveExportPort) SerializableType() string { return "DirectiveExportPort" }

// Dependencies implements Directive.
func (d DirectiveExportPort) Dependencies() ([]BuildDefinition, error) { return nil, nil }

// AsFragments implements Directive.
func (d DirectiveExportPort) AsFragments(ctx BuildContext, special SpecialDirectiveHandlers) ([]config.Fragment, error) {
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

// Dependencies implements Directive.
func (d DirectiveEnvironment) Dependencies() ([]BuildDefinition, error) { return nil, nil }

// AsFragments implements Directive.
func (d DirectiveEnvironment) AsFragments(ctx BuildContext, special SpecialDirectiveHandlers) ([]config.Fragment, error) {
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

// Dependencies implements Directive.
func (d DirectiveBuiltin) Dependencies() ([]BuildDefinition, error) { return nil, nil }

// AsFragments implements Directive.
func (d DirectiveBuiltin) AsFragments(ctx BuildContext, special SpecialDirectiveHandlers) ([]config.Fragment, error) {
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
func (d DirectiveList) AsFragments(ctx BuildContext, special SpecialDirectiveHandlers) ([]config.Fragment, error) {
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

// Dependencies implements Directive.
func (d DirectiveList) Dependencies() ([]BuildDefinition, error) {
	var ret []BuildDefinition

	for _, dir := range d.Items {
		deps, err := dir.Dependencies()
		if err != nil {
			return nil, err
		}

		ret = append(ret, deps...)
	}

	return ret, nil
}

// SerializableType implements Directive.
func (d DirectiveList) SerializableType() string { return "DirectiveList" }

type DirectiveAddPackage struct {
	Name PackageQuery
}

// AsFragments implements Directive.
func (d DirectiveAddPackage) AsFragments(ctx BuildContext, special SpecialDirectiveHandlers) ([]config.Fragment, error) {
	return nil, fmt.Errorf("DirectiveAddPackage cannot be represented as a fragment")
}

// Dependencies implements Directive.
func (d DirectiveAddPackage) Dependencies() ([]BuildDefinition, error) { return nil, nil }

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
func (d DirectiveInteraction) AsFragments(ctx BuildContext, special SpecialDirectiveHandlers) ([]config.Fragment, error) {
	return nil, fmt.Errorf("DirectiveInteraction cannot be represented as a fragment")
}

// Dependencies implements Directive.
func (d DirectiveInteraction) Dependencies() ([]BuildDefinition, error) { return nil, nil }

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
func (d DirectiveDefaultInteractive) AsFragments(ctx BuildContext, special SpecialDirectiveHandlers) ([]config.Fragment, error) {
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

// Dependencies implements Directive.
func (d DirectiveDefaultInteractive) Dependencies() ([]BuildDefinition, error) { return nil, nil }

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
func (d DirectiveMountHostDirectory) AsFragments(ctx BuildContext, special SpecialDirectiveHandlers) ([]config.Fragment, error) {
	return []config.Fragment{
		{MountHostDirectory: &config.MountHostDirectoryFragment{HostDirectory: d.HostDirectory, Writable: d.Writable}},
	}, nil
}

// Dependencies implements Directive.
func (d DirectiveMountHostDirectory) Dependencies() ([]BuildDefinition, error) { return nil, nil }

// SerializableType implements Directive.
func (d DirectiveMountHostDirectory) SerializableType() string { return "DirectiveMountHostDirectory" }

// Tag implements Directive.
func (d DirectiveMountHostDirectory) Tag() string {
	return fmt.Sprintf("DirectiveMountHostDirectory_%s", d.HostDirectory)
}

type DirectiveKernel struct {
	Kernel    BuildDefinition
	Initramfs BuildDefinition
}

// AsFragments implements Directive.
func (d DirectiveKernel) AsFragments(ctx BuildContext, special SpecialDirectiveHandlers) ([]config.Fragment, error) {
	var (
		kernelFilename    string
		initramfsFilename string
	)

	if d.Kernel != nil {
		kernelArt, err := ctx.BuildChild(d.Kernel)
		if err != nil {
			return nil, err
		}

		kernelRes, err := kernelArt.Default()
		if err != nil {
			return nil, err
		}

		kernelFilename, err = ctx.HostFilenameFromFile(kernelRes)
		if err != nil {
			return nil, err
		}
	}

	if d.Initramfs != nil {
		initramfsArt, err := ctx.BuildChild(d.Initramfs)
		if err != nil {
			return nil, err
		}

		initramfsRes, err := initramfsArt.Default()
		if err != nil {
			return nil, err
		}

		initramfsFilename, err = ctx.HostFilenameFromFile(initramfsRes)
		if err != nil {
			return nil, err
		}
	}

	return []config.Fragment{
		{Kernel: &config.KernelFragment{
			KernelFilename:    kernelFilename,
			InitramfsFilename: initramfsFilename,
		}},
	}, nil
}

// Dependencies implements Directive.
func (d DirectiveKernel) Dependencies() ([]BuildDefinition, error) {
	return []BuildDefinition{d.Kernel, d.Initramfs}, nil
}

// SerializableType implements Directive.
func (d DirectiveKernel) SerializableType() string { return "DirectiveKernel" }

type DirectiveAddInitScript struct {
	GuestFilename string
}

// AsFragments implements Directive.
func (d DirectiveAddInitScript) AsFragments(ctx BuildContext, special SpecialDirectiveHandlers) ([]config.Fragment, error) {
	return []config.Fragment{
		{AddInitScript: &config.AddInitScriptFragment{GuestFilename: d.GuestFilename}},
	}, nil
}

// Dependencies implements Directive.
func (d DirectiveAddInitScript) Dependencies() ([]BuildDefinition, error) { return nil, nil }

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

func (d *StarDirective) String() string      { return d.Type() }
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

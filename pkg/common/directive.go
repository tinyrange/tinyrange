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
}

type Directive interface {
	DependencyNode
	Tag() string
	AsFragments(ctx BuildContext) ([]config.Fragment, error)
}

type DirectiveRunCommand struct {
	Command string
}

// Dependencies implements Directive.
func (d DirectiveRunCommand) Dependencies(ctx BuildContext) ([]DependencyNode, error) {
	return []DependencyNode{}, nil
}

// SerializableType implements Directive.
func (d DirectiveRunCommand) SerializableType() string { return "DirectiveRunCommand" }

// AsFragments implements Directive.
func (d DirectiveRunCommand) AsFragments(ctx BuildContext) ([]config.Fragment, error) {
	return []config.Fragment{
		{RunCommand: &config.RunCommandFragment{Command: string(d.Command)}},
	}, nil
}

// Tag implements Directive.
func (d DirectiveRunCommand) Tag() string {
	return fmt.Sprintf("RunCommand_%s", strings.ReplaceAll(string(d.Command), " ", "_"))
}

type DirectiveAddFile struct {
	Filename   string
	Definition BuildDefinition
	Contents   []byte
	Executable bool
}

// Dependencies implements Directive.
func (d DirectiveAddFile) Dependencies(ctx BuildContext) ([]DependencyNode, error) {
	return []DependencyNode{d.Definition}, nil
}

// SerializableType implements Directive.
func (d DirectiveAddFile) SerializableType() string { return "DirectiveAddFile" }

// AsFragments implements Directive.
func (d DirectiveAddFile) AsFragments(ctx BuildContext) ([]config.Fragment, error) {
	if d.Definition != nil {
		res, err := ctx.BuildChild(d.Definition)
		if err != nil {
			return nil, err
		}

		digest := res.Digest()

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
func (d DirectiveLocalFile) AsFragments(ctx BuildContext) ([]config.Fragment, error) {
	return []config.Fragment{
		{LocalFile: &config.LocalFileFragment{
			HostFilename:  d.HostFilename,
			GuestFilename: d.Filename,
		}},
	}, nil
}

// Dependencies implements Directive.
func (d DirectiveLocalFile) Dependencies(ctx BuildContext) ([]DependencyNode, error) {
	return []DependencyNode{}, nil
}

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

// Dependencies implements Directive.
func (d DirectiveArchive) Dependencies(ctx BuildContext) ([]DependencyNode, error) {
	return []DependencyNode{d.Definition}, nil
}

// SerializableType implements Directive.
func (d DirectiveArchive) SerializableType() string { return "DirectiveArchive" }

// AsFragments implements Directive.
func (d DirectiveArchive) AsFragments(ctx BuildContext) ([]config.Fragment, error) {
	res, err := ctx.BuildChild(d.Definition)
	if err != nil {
		return nil, err
	}

	digest := res.Digest()

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

// Dependencies implements Directive.
func (d DirectiveExportPort) Dependencies(ctx BuildContext) ([]DependencyNode, error) {
	return []DependencyNode{}, nil
}

// SerializableType implements Directive.
func (d DirectiveExportPort) SerializableType() string { return "DirectiveExportPort" }

// AsFragments implements Directive.
func (d DirectiveExportPort) AsFragments(ctx BuildContext) ([]config.Fragment, error) {
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

// Dependencies implements Directive.
func (d DirectiveEnvironment) Dependencies(ctx BuildContext) ([]DependencyNode, error) {
	return []DependencyNode{}, nil
}

// SerializableType implements Directive.
func (d DirectiveEnvironment) SerializableType() string { return "DirectiveEnvironment" }

// AsFragments implements Directive.
func (d DirectiveEnvironment) AsFragments(ctx BuildContext) ([]config.Fragment, error) {
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

// Dependencies implements Directive.
func (d DirectiveBuiltin) Dependencies(ctx BuildContext) ([]DependencyNode, error) {
	return []DependencyNode{}, nil
}

// SerializableType implements Directive.
func (d DirectiveBuiltin) SerializableType() string { return "DirectiveFragment" }

// AsFragments implements Directive.
func (d DirectiveBuiltin) AsFragments(ctx BuildContext) ([]config.Fragment, error) {
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
func (d DirectiveList) AsFragments(ctx BuildContext) ([]config.Fragment, error) {
	var ret []config.Fragment

	for _, dir := range d.Items {
		frags, err := dir.AsFragments(ctx)
		if err != nil {
			return nil, err
		}

		ret = append(ret, frags...)
	}

	return ret, nil
}

// Dependencies implements Directive.
func (d DirectiveList) Dependencies(ctx BuildContext) ([]DependencyNode, error) {
	var ret []DependencyNode

	for _, dir := range d.Items {
		ret = append(ret, dir)
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
func (d DirectiveAddPackage) AsFragments(ctx BuildContext) ([]config.Fragment, error) {
	return nil, fmt.Errorf("DirectiveAddPackage cannot be represented as a fragment")
}

// Dependencies implements Directive.
func (d DirectiveAddPackage) Dependencies(ctx BuildContext) ([]DependencyNode, error) {
	return nil, nil
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
func (d DirectiveInteraction) AsFragments(ctx BuildContext) ([]config.Fragment, error) {
	return nil, fmt.Errorf("DirectiveInteraction cannot be represented as a fragment")
}

// Dependencies implements Directive.
func (d DirectiveInteraction) Dependencies(ctx BuildContext) ([]DependencyNode, error) {
	return nil, nil
}

// SerializableType implements Directive.
func (d DirectiveInteraction) SerializableType() string { return "DirectiveInteraction" }

// Tag implements Directive.
func (d DirectiveInteraction) Tag() string {
	return fmt.Sprintf("DirectiveInteraction_%s", d.Interaction)
}

var (
	_ Directive = DirectiveRunCommand{}
	_ Directive = DirectiveAddFile{}
	_ Directive = DirectiveLocalFile{}
	_ Directive = DirectiveArchive{}
	_ Directive = DirectiveExportPort{}
	_ Directive = DirectiveEnvironment{}
	_ Directive = DirectiveBuiltin{}
	_ Directive = DirectiveList{}
	_ Directive = DirectiveAddPackage{}
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
	RunCommand  func(dir DirectiveRunCommand) error
	AddPackage  func(dir DirectiveAddPackage) error
	Environment func(dir DirectiveEnvironment) error
	Interaction func(dir DirectiveInteraction) error
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

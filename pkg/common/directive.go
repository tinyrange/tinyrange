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

var (
	_ Directive = DirectiveRunCommand{}
	_ Directive = DirectiveAddFile{}
	_ Directive = DirectiveArchive{}
	_ Directive = DirectiveExportPort{}
	_ Directive = DirectiveEnvironment{}
)

type StarDirective struct {
	Directive
}

func (d *StarDirective) String() string      { return d.Tag() }
func (d *StarDirective) Type() string        { return fmt.Sprintf("%T", d.Directive) }
func (*StarDirective) Hash() (uint32, error) { return 0, fmt.Errorf("Directive is not hashable") }
func (*StarDirective) Truth() starlark.Bool  { return starlark.True }
func (*StarDirective) Freeze()               {}

var (
	_ starlark.Value = &StarDirective{}
)

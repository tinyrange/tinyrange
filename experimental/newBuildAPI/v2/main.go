package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/tinyrange/tinyrange/pkg/log"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

type FileHandle interface {
	io.Reader
}
type FileContents interface {
	Open() (FileHandle, error)
}
type File interface {
	FileContents
	Name() string
}
type Directory interface {
	File

	AddFile(file File) (Directory, error)
	ReadDir() ([]File, error)
}
type Context interface {
	Build(def BuildDefinition) (Artifact, error)
}
type BuildDefinition interface {
	Build(ctx Context) (Artifact, error)
}
type Artifact interface{}
type SerializableValue interface{}
type Fragment interface{}
type Directive interface {
	AsFragments(ctx Context) ([]Fragment, error)
}

type DirectiveList []Directive

type InteractiveDirective struct {
}

// AsFragments implements Directive.
func (i InteractiveDirective) AsFragments(ctx Context) ([]Fragment, error) {
	return []Fragment{"interactive"}, nil
}

var (
	_ Directive = InteractiveDirective{}
)

type AddFileFragment struct {
	Name string
}

type DirectoryDirective struct {
	Directory
}

// AsFragments implements Directive.
func (d *DirectoryDirective) AsFragments(ctx Context) ([]Fragment, error) {
	var frags []Fragment

	var walkDirectory func(d Directory, prefix string) error

	walkDirectory = func(d Directory, prefix string) error {
		ents, err := d.ReadDir()
		if err != nil {
			return err
		}

		for _, ent := range ents {
			_ = ent
		}

		return nil
	}

	if err := walkDirectory(d.Directory, "/"); err != nil {
		return nil, err
	}

	return frags, nil
}

var (
	_ Directive = &DirectoryDirective{}
)

type alpineFetcher struct {
}

// AsFragments implements Directive.
func (a *alpineFetcher) AsFragments(ctx Context) ([]Fragment, error) {
	return []Fragment{"alpine"}, nil
}

func (a *alpineFetcher) AsDirectives() (DirectiveList, error) {
	return DirectiveList{a}, nil
}

// Binary implements starlark.HasBinary.
func (a *alpineFetcher) Binary(op syntax.Token, y starlark.Value, side starlark.Side) (starlark.Value, error) {
	if op == syntax.PLUS && side == starlark.Left {
		switch y := y.(type) {
		case *StarDirectory:
			frags, err := a.AsDirectives()
			if err != nil {
				return nil, err
			}

			return &StarDirectiveList{
				DirectiveList: append(frags, &DirectoryDirective{y.Directory}),
			}, nil
		default:
			return nil, fmt.Errorf("cannot add %s to %s", y.Type(), a.Type())
		}
	} else {
		return nil, fmt.Errorf("operation %s not implemented", op)
	}
}

func (a *alpineFetcher) Freeze() {}
func (a *alpineFetcher) Hash() (uint32, error) {
	return 0, fmt.Errorf("hash for %s unimplemented", a.Type())
}
func (a *alpineFetcher) String() string       { return a.Type() }
func (a *alpineFetcher) Truth() starlark.Bool { return starlark.True }
func (a *alpineFetcher) Type() string         { return "alpineFetcher" }

var (
	_ starlark.Value     = &alpineFetcher{}
	_ starlark.HasBinary = &alpineFetcher{}
	_ Directive          = &alpineFetcher{}
)

type alpineFetcherCollection struct {
}

// Attr implements starlark.HasAttrs.
func (a *alpineFetcherCollection) Attr(name string) (starlark.Value, error) {
	if name == "latest" {
		return &alpineFetcher{}, nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (a *alpineFetcherCollection) AttrNames() []string {
	return []string{"latest"}
}

func (a *alpineFetcherCollection) Freeze() {}
func (a *alpineFetcherCollection) Hash() (uint32, error) {
	return 0, fmt.Errorf("hash for %s unimplemented", a.Type())
}
func (a *alpineFetcherCollection) String() string       { return a.Type() }
func (a *alpineFetcherCollection) Truth() starlark.Bool { return starlark.True }
func (a *alpineFetcherCollection) Type() string         { return "alpineFetcherCollection" }

var (
	_ starlark.Value    = &alpineFetcherCollection{}
	_ starlark.HasAttrs = &alpineFetcherCollection{}
)

type memoryFile struct {
	FileContents
	name string
}

func (a *memoryFile) Name() string {
	return a.name
}

type memoryFileContents struct {
	contents string
}

// Open implements FileContents.
func (m *memoryFileContents) Open() (FileHandle, error) {
	return bytes.NewReader([]byte(m.contents)), nil
}

var (
	_ FileContents = &memoryFileContents{}
)

type memoryDirectory struct {
	*memoryFile
	files map[string]File
}

// AddFile implements Directory.
func (m *memoryDirectory) AddFile(file File) (Directory, error) {
	// copy all the files from the current directory
	newFiles := map[string]File{}
	for k, v := range m.files {
		newFiles[k] = v
	}

	// add the new file
	newFiles[file.Name()] = file

	return &memoryDirectory{
		memoryFile: m.memoryFile,
		files:      newFiles,
	}, nil
}

// ReadDir implements Directory.
func (m *memoryDirectory) ReadDir() ([]File, error) {
	var files []File

	for _, f := range m.files {
		files = append(files, f)
	}

	return files, nil
}

// Open implements FileContents.
func (m *memoryDirectory) Open() (FileHandle, error) {
	return nil, fmt.Errorf("cannot open directory")
}

var (
	_ Directory = &memoryDirectory{}
)

func newMemoryDirectory(name string) *memoryDirectory {
	return &memoryDirectory{
		memoryFile: &memoryFile{
			name: name,
		},
		files: map[string]File{},
	}
}

type StarFile struct {
	File
}

// Attr implements starlark.HasAttrs.
func (a *StarFile) Attr(name string) (starlark.Value, error) {
	if name == "rename" {
		return starlark.NewBuiltin("File.rename", func(
			thread *starlark.Thread,
			builtin *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				name string
			)

			if err := starlark.UnpackArgs(builtin.Name(), args, kwargs,
				"name", &name,
			); err != nil {
				return starlark.None, err
			}

			return &StarFile{
				File: &memoryFile{
					name:         name,
					FileContents: a.File,
				},
			}, nil
		}), nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (a *StarFile) AttrNames() []string {
	return []string{"rename"}
}

func (a *StarFile) Freeze() {}
func (a *StarFile) Hash() (uint32, error) {
	return 0, fmt.Errorf("hash for %s unimplemented", a.Type())
}
func (a *StarFile) String() string       { return a.Type() }
func (a *StarFile) Truth() starlark.Bool { return starlark.True }
func (a *StarFile) Type() string         { return "StarFile" }

var (
	_ starlark.Value    = &StarFile{}
	_ starlark.HasAttrs = &StarFile{}
)

type StarDirectory struct {
	Directory
}

// Binary implements starlark.HasBinary.
func (a *StarDirectory) Binary(op syntax.Token, y starlark.Value, side starlark.Side) (starlark.Value, error) {
	if op == syntax.PLUS && side == starlark.Left {
		switch y := y.(type) {
		case *StarFile:
			newDir, err := a.AddFile(y.File)
			if err != nil {
				return nil, err
			}

			return &StarDirectory{
				Directory: newDir,
			}, nil
		default:
			return nil, fmt.Errorf("cannot add %s to %s", y.Type(), a.Type())
		}
	} else {
		return nil, fmt.Errorf("operation %s not implemented", op)
	}
}

func (a *StarDirectory) Freeze() {}
func (a *StarDirectory) Hash() (uint32, error) {
	return 0, fmt.Errorf("hash for %s unimplemented", a.Type())
}
func (a *StarDirectory) String() string       { return a.Type() }
func (a *StarDirectory) Truth() starlark.Bool { return starlark.True }
func (a *StarDirectory) Type() string         { return "StarDirectory" }

var (
	_ starlark.Value     = &StarDirectory{}
	_ starlark.HasBinary = &StarDirectory{}
)

type StarDirectiveList struct {
	DirectiveList
}

func (a *StarDirectiveList) Freeze() {}
func (a *StarDirectiveList) Hash() (uint32, error) {
	return 0, fmt.Errorf("hash for %s unimplemented", a.Type())
}
func (a *StarDirectiveList) String() string       { return a.Type() }
func (a *StarDirectiveList) Truth() starlark.Bool { return starlark.True }
func (a *StarDirectiveList) Type() string         { return "StarDirectiveList" }

var (
	_ starlark.Value = &StarDirectiveList{}
)

type StarBuildDefinition struct {
	BuildDefinition
}

// Attr implements starlark.HasAttrs.
func (a *StarBuildDefinition) Attr(name string) (starlark.Value, error) {
	if name == "default" {
		// TODO(joshua): implement proper default
		return &StarFile{File: &memoryFile{
			FileContents: &memoryFileContents{
				contents: "default",
			},
			name: "default",
		}}, nil
	} else {
		return nil, fmt.Errorf("attribute %s not found", name)
	}
}

// AttrNames implements starlark.HasAttrs.
func (a *StarBuildDefinition) AttrNames() []string {
	return []string{"default"}
}

func (a *StarBuildDefinition) Freeze() {}
func (a *StarBuildDefinition) Hash() (uint32, error) {
	return 0, fmt.Errorf("hash for %s unimplemented", a.Type())
}
func (a *StarBuildDefinition) String() string       { return a.Type() }
func (a *StarBuildDefinition) Truth() starlark.Bool { return starlark.True }
func (a *StarBuildDefinition) Type() string         { return "StarBuildDefinition" }

var (
	_ starlark.Value    = &StarBuildDefinition{}
	_ starlark.HasAttrs = &StarBuildDefinition{}
)

type StarlarkBuildDefinitionInstance struct {
	def  *StarlarkBuildDefinition
	args []SerializableValue
}

// Build implements BuildDefinition.
func (s *StarlarkBuildDefinitionInstance) Build(ctx Context) (Artifact, error) {
	panic("unimplemented")
}

var (
	_ BuildDefinition = &StarlarkBuildDefinitionInstance{}
)

type StarlarkBuildDefinition struct {
	filename string
	fnName   string
}

// CallInternal implements starlark.Callable.
func (a *StarlarkBuildDefinition) CallInternal(
	thread *starlark.Thread,
	args starlark.Tuple,
	kwargs []starlark.Tuple,
) (starlark.Value, error) {
	var serializableArgs []SerializableValue

	for _, arg := range args {
		serializableArgs = append(serializableArgs, arg)
	}

	return &StarBuildDefinition{
		BuildDefinition: &StarlarkBuildDefinitionInstance{
			def:  a,
			args: serializableArgs,
		},
	}, nil
}

// Name implements starlark.Callable.
func (a *StarlarkBuildDefinition) Name() string {
	return fmt.Sprintf("%s:%s", a.filename, a.fnName)
}

func (a *StarlarkBuildDefinition) Freeze() {}
func (a *StarlarkBuildDefinition) Hash() (uint32, error) {
	return 0, fmt.Errorf("hash for %s unimplemented", a.Type())
}
func (a *StarlarkBuildDefinition) String() string       { return a.Type() }
func (a *StarlarkBuildDefinition) Truth() starlark.Bool { return starlark.True }
func (a *StarlarkBuildDefinition) Type() string         { return "StarlarkBuildDefinition" }

var (
	_ starlark.Value    = &StarlarkBuildDefinition{}
	_ starlark.Callable = &StarlarkBuildDefinition{}
)

type BuildVMDefinition struct {
	Directives DirectiveList
}

// Build implements BuildDefinition.
func (b *BuildVMDefinition) Build(ctx Context) (Artifact, error) {
	var frags []Fragment
	for _, d := range b.Directives {
		f, err := d.AsFragments(ctx)
		if err != nil {
			return nil, err
		}

		frags = append(frags, f...)
	}

	log.Info("build", "fragments", frags)

	return nil, fmt.Errorf("unimplemented")
}

var (
	_ BuildDefinition = &BuildVMDefinition{}
)

type StarBuildVMDefinition struct {
	*BuildVMDefinition
}

// Attr implements starlark.HasAttrs.
func (a *StarBuildVMDefinition) Attr(name string) (starlark.Value, error) {
	if name == "interactive" {
		return starlark.NewBuiltin("BuildVMDefinition.interactive", func(
			thread *starlark.Thread,
			builtin *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			return &StarBuildVMDefinition{
				BuildVMDefinition: &BuildVMDefinition{
					Directives: append(a.BuildVMDefinition.Directives, InteractiveDirective{}),
				},
			}, nil
		}), nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (a *StarBuildVMDefinition) AttrNames() []string {
	return []string{"interactive"}
}

func (a *StarBuildVMDefinition) Freeze() {}
func (a *StarBuildVMDefinition) Hash() (uint32, error) {
	return 0, fmt.Errorf("hash for %s unimplemented", a.Type())
}
func (a *StarBuildVMDefinition) String() string       { return a.Type() }
func (a *StarBuildVMDefinition) Truth() starlark.Bool { return starlark.True }
func (a *StarBuildVMDefinition) Type() string         { return "StarBuildVMDefinition" }

var (
	_ starlark.Value    = &StarBuildVMDefinition{}
	_ starlark.HasAttrs = &StarBuildVMDefinition{}
)

type StarArtifact struct {
	Artifact
}

func (a *StarArtifact) Freeze() {}
func (a *StarArtifact) Hash() (uint32, error) {
	return 0, fmt.Errorf("hash for %s unimplemented", a.Type())
}
func (a *StarArtifact) String() string       { return a.Type() }
func (a *StarArtifact) Truth() starlark.Bool { return starlark.True }
func (a *StarArtifact) Type() string         { return "StarArtifact" }

var (
	_ starlark.Value = &StarArtifact{}
)

type loadedFile struct {
	definitions map[string]starlark.Value
}

type context struct {
	parent *context
	files  map[string]*loadedFile
}

// addFile adds a file to the context.
func (c *context) addFile(filename string, defs starlark.StringDict) error {
	loaded := &loadedFile{
		definitions: map[string]starlark.Value{},
	}

	for k, v := range defs {
		loaded.definitions[k] = v
	}

	c.files[filename] = loaded

	return nil
}

// get retrieves a value from the context.
func (c *context) get(filename string, name string) (starlark.Value, error) {
	if c.parent != nil {
		if v, err := c.parent.get(filename, name); err == nil {
			return v, nil
		}
	}

	if c.files == nil {
		return nil, fmt.Errorf("file %s not loaded", filename)
	}

	if loaded, ok := c.files[filename]; ok {
		if v, ok := loaded.definitions[name]; ok {
			return v, nil
		} else {
			return nil, fmt.Errorf("value %s not found in %s", name, filename)
		}
	}

	return nil, fmt.Errorf("file %s not loaded", filename)
}

// childContext creates a new child context.
func (c *context) childContext() *context {
	return &context{
		parent: c,
		files:  nil,
	}
}

func (c *context) Build(def BuildDefinition) (Artifact, error) {
	return def.Build(c.childContext())
}

// Attr implements starlark.HasAttrs.
func (ctx *context) Attr(name string) (starlark.Value, error) {
	if name == "build" {
		return starlark.NewBuiltin("Context.build", func(
			thread *starlark.Thread,
			builtin *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				def starlark.Value
			)

			if err := starlark.UnpackArgs(builtin.Name(), args, kwargs,
				"def", &def,
			); err != nil {
				return starlark.None, err
			}

			buildDef, ok := def.(BuildDefinition)
			if !ok {
				return nil, fmt.Errorf("expected BuildDefinition, got %s", def.Type())
			}

			art, err := ctx.Build(buildDef)
			if err != nil {
				return nil, err
			}

			return &StarArtifact{
				Artifact: art,
			}, nil
		}), nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (ctx *context) AttrNames() []string {
	return []string{"build"}
}

func (a *context) Freeze() {}
func (a *context) Hash() (uint32, error) {
	return 0, fmt.Errorf("hash for %s unimplemented", a.Type())
}
func (a *context) String() string       { return a.Type() }
func (a *context) Truth() starlark.Bool { return starlark.True }
func (a *context) Type() string         { return "Context" }

var (
	_ starlark.Value    = &context{}
	_ starlark.HasAttrs = &context{}
	_ Context           = &context{}
)

func newContext() *context {
	return &context{
		files: map[string]*loadedFile{},
	}
}

func appMain() error {
	flag.Parse()

	filename := flag.Arg(0)

	universe := starlark.StringDict{}

	universe["fetcher"] = starlark.NewBuiltin("fetcher", func(
		thread *starlark.Thread,
		builtin *starlark.Builtin,
		args starlark.Tuple,
		kwargs []starlark.Tuple,
	) (starlark.Value, error) {
		var (
			name string
		)

		if err := starlark.UnpackArgs(builtin.Name(), args, kwargs,
			"name", &name,
		); err != nil {
			return starlark.None, err
		}

		if name == "alpine" {
			return &alpineFetcherCollection{}, nil
		} else {
			return starlark.None, fmt.Errorf("fetcher %s not implemented", name)
		}
	})

	universe["directory"] = starlark.NewBuiltin("directory", func(
		thread *starlark.Thread,
		builtin *starlark.Builtin,
		args starlark.Tuple,
		kwargs []starlark.Tuple,
	) (starlark.Value, error) {
		var (
			name string
		)

		if err := starlark.UnpackArgs(builtin.Name(), args, kwargs,
			"name", &name,
		); err != nil {
			return starlark.None, err
		}

		return &StarDirectory{
			Directory: newMemoryDirectory(name),
		}, nil
	})

	universe["file"] = starlark.NewBuiltin("file", func(
		thread *starlark.Thread,
		builtin *starlark.Builtin,
		args starlark.Tuple,
		kwargs []starlark.Tuple,
	) (starlark.Value, error) {
		var (
			name     string
			contents string
		)

		if err := starlark.UnpackArgs(builtin.Name(), args, kwargs,
			"name", &name,
			"contents", &contents,
		); err != nil {
			return starlark.None, err
		}

		return &StarFile{
			File: &memoryFile{
				name: name,
				FileContents: &memoryFileContents{
					contents: contents,
				},
			},
		}, nil
	})

	universe["build_vm"] = starlark.NewBuiltin("build_vm", func(
		thread *starlark.Thread,
		builtin *starlark.Builtin,
		args starlark.Tuple,
		kwargs []starlark.Tuple,
	) (starlark.Value, error) {
		var (
			directives *StarDirectiveList
		)

		if err := starlark.UnpackArgs(builtin.Name(), args, kwargs,
			"directives", &directives,
		); err != nil {
			return starlark.None, err
		}

		return &StarBuildVMDefinition{
			BuildVMDefinition: &BuildVMDefinition{
				Directives: directives.DirectiveList,
			},
		}, nil
	})

	universe["build"] = starlark.NewBuiltin("build", func(
		thread *starlark.Thread,
		builtin *starlark.Builtin,
		args starlark.Tuple,
		kwargs []starlark.Tuple,
	) (starlark.Value, error) {
		var (
			fn *starlark.Function
		)

		if err := starlark.UnpackArgs(builtin.Name(), args, kwargs,
			"fn", &fn,
		); err != nil {
			return starlark.None, err
		}

		return &StarlarkBuildDefinition{
			filename: fn.Position().Filename(),
			fnName:   fn.Name(),
		}, nil
	})

	opts := &syntax.FileOptions{}

	thread := &starlark.Thread{
		Name: "main",
		Load: func(thread *starlark.Thread, module string) (starlark.StringDict, error) {
			return nil, fmt.Errorf("module not found: %s", module)
		},
	}

	ctx := newContext()

	top, err := starlark.ExecFileOptions(opts, thread, filename, nil, universe)
	if err != nil {
		return err
	}

	if err := ctx.addFile(filename, top); err != nil {
		return err
	}

	main, err := ctx.get(filename, "main")
	if err != nil {
		return err
	}

	if _, err := starlark.Call(thread, main, starlark.Tuple{ctx}, nil); err != nil {
		return err
	}

	return nil
}

func main() {
	if err := appMain(); err != nil {
		log.Error("fatal", "error", err)
		os.Exit(1)
	}
}

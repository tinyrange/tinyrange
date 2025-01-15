package builder

import (
	"fmt"
	"slices"
	"strings"

	"github.com/tinyrange/tinyrange/pkg/builder/oci"
	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/config"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/hash"
	"go.starlark.net/starlark"
)

type OciManifest []struct {
	Config   string   `json:"Config"`
	RepoTags []string `json:"RepoTags"`
	Layers   []string `json:"Layers"`
}

type readOciImageDefinition struct {
	params ReadOciImageParameters
}

// Dependencies implements common.BuildDefinition.
func (r *readOciImageDefinition) Dependencies() ([]common.BuildDefinition, error) {
	return []common.BuildDefinition{r.params.Base}, nil
}

// NeedsBuild implements common.BuildDefinition.
func (r *readOciImageDefinition) NeedsBuild(ctx common.BuildContext) (bool, error) {
	return false, nil
}

// Params implements common.BuildDefinition.
func (r *readOciImageDefinition) Params() hash.SerializableValue { return r.params }
func (r *readOciImageDefinition) SerializableType() string       { return "ReadOciImageDefinition" }
func (r *readOciImageDefinition) Create(params hash.SerializableValue) hash.Definition {
	return &readOciImageDefinition{params: params.(ReadOciImageParameters)}
}

// String implements common.BuildDefinition.
func (r *readOciImageDefinition) String() string {
	tag := []string{"ReadOciImage"}
	tag = append(tag, r.params.Base.String())
	return strings.Join(tag, "_")
}

// AsFragments implements common.Directive.
func (r *readOciImageDefinition) AsFragments(ctx common.BuildContext, special common.SpecialDirectiveHandlers) ([]config.Fragment, error) {
	art, err := ctx.BuildChild(r)
	if err != nil {
		return nil, err
	}

	res, err := art.Default()
	if err != nil {
		return nil, err
	}

	var def fetchOciImageDefinition

	if err := ParseJsonFromFile(res, &def); err != nil {
		return nil, err
	}

	var ret []config.Fragment

	for _, archive := range def.LayerArchives {
		file, err := ctx.FileFromDigest(archive)
		if err != nil {
			return nil, err
		}

		filename, err := ctx.HostFilenameFromFile(file)
		if err != nil {
			return nil, err
		}

		ret = append(ret, config.Fragment{Archive: &config.ArchiveFragment{HostFilename: filename}})
	}

	slices.Reverse(ret)

	if def.Config.Config.Env != nil {
		ret = append(ret, config.Fragment{Environment: &config.EnvironmentFragment{Variables: def.Config.Config.Env}})
	}

	return ret, nil
}

// ToStarlark implements common.BuildDefinition.
func (r *readOciImageDefinition) ToStarlark(ctx common.BuildContext, artifact common.BuildArtifact) (starlark.Value, error) {
	var def fetchOciImageDefinition

	return def.ToStarlark(ctx, artifact)
}

// Build implements common.BuildDefinition.
func (r *readOciImageDefinition) Build(ctx common.BuildContext) error {
	child, err := ctx.BuildChild(r.params.Base)
	if err != nil {
		return err
	}

	childFile, err := child.Default()
	if err != nil {
		return err
	}

	ark, err := filesystem.ReadArchiveFromFile(childFile)
	if err != nil {
		return err
	}

	ents, err := ark.Entries()
	if err != nil {
		return err
	}

	filenames := make(map[string]filesystem.Entry)

	for _, ent := range ents {
		filenames[ent.Name()] = ent
	}

	var manifest OciManifest

	if err := ParseJsonFromFile(filenames["manifest.json"], &manifest); err != nil {
		return err
	}

	if len(manifest) != 1 {
		return fmt.Errorf("no manifest found or multiple manifests found")
	}

	mainManifest := manifest[0]

	var config oci.ImageConfig

	if err := ParseJsonFromFile(filenames[mainManifest.Config], &config); err != nil {
		return err
	}

	out := &fetchOciImageDefinition{}

	for _, layer := range mainManifest.Layers {
		layerDef, err := newDefinitionFromFile(filenames[layer])
		if err != nil {
			return err
		}

		readArchiveDef := newReadArchiveBuildDefinition(layerDef, ".tar$oci.gz")

		layerArtifact, err := ctx.BuildChild(readArchiveDef)
		if err != nil {
			return err
		}

		layerFile, err := layerArtifact.Default()
		if err != nil {
			return err
		}

		layerFileDigest, err := ctx.DigestFromFile(layerFile)
		if err != nil {
			return err
		}

		out.LayerArchives = append(out.LayerArchives, layerFileDigest)
	}

	out.Config = config

	return ctx.WriteDefault(out)
}

var (
	_ common.BuildDefinition = &readOciImageDefinition{}
)

func newReadOCIImageDefinition(base common.BuildDefinition) ReadOCIImageDefinition {
	return &readOciImageDefinition{
		params: ReadOciImageParameters{
			Base: base,
		},
	}
}

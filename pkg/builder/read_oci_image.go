package builder

import (
	"fmt"
	"slices"
	"strings"
	"time"

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
func (r *readOciImageDefinition) Dependencies(ctx common.BuildContext1) ([]common.DependencyNode, error) {
	return []common.DependencyNode{r.params.Base}, nil
}

// NeedsBuild implements common.BuildDefinition.
func (r *readOciImageDefinition) NeedsBuild(ctx common.BuildContext1, cacheTime time.Time) (bool, error) {
	return ctx.NeedsBuild(r.params.Base)
}

// Params implements common.BuildDefinition.
func (r *readOciImageDefinition) Params() hash.SerializableValue { return r.params }
func (r *readOciImageDefinition) SerializableType() string       { return "ReadOciImageDefinition" }
func (r *readOciImageDefinition) Create(params hash.SerializableValue) hash.Definition {
	return &readOciImageDefinition{params: params.(ReadOciImageParameters)}
}

// Tag implements common.BuildDefinition.
func (r *readOciImageDefinition) Tag() string {
	tag := []string{"ReadOciImage"}
	tag = append(tag, r.params.Base.Tag())
	return strings.Join(tag, "_")
}

// AsFragments implements common.Directive.
func (r *readOciImageDefinition) AsFragments(ctx common.BuildContext1, special common.SpecialDirectiveHandlers) ([]config.Fragment, error) {
	res, err := ctx.BuildChild(r)
	if err != nil {
		return nil, err
	}

	var def fetchOciImageDefinition

	if err := ParseJsonFromFile(res, &def); err != nil {
		return nil, err
	}

	var ret []config.Fragment

	for _, archive := range def.LayerArchives {
		filename, err := ctx.FilenameFromDigest(archive)
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
func (r *readOciImageDefinition) ToStarlark(ctx common.BuildContext1, result filesystem.File) (starlark.Value, error) {
	var def fetchOciImageDefinition

	return def.ToStarlark(ctx, result)
}

// Build implements common.BuildDefinition.
func (r *readOciImageDefinition) Build(ctx common.BuildContext1) (common.BuildResult, error) {
	child, err := ctx.BuildChild(r.params.Base)
	if err != nil {
		return nil, err
	}

	ark, err := filesystem.ReadArchiveFromFile(child)
	if err != nil {
		return nil, err
	}

	ents, err := ark.Entries()
	if err != nil {
		return nil, err
	}

	filenames := make(map[string]filesystem.Entry)

	for _, ent := range ents {
		filenames[ent.Name()] = ent
	}

	var manifest OciManifest

	if err := ParseJsonFromFile(filenames["manifest.json"], &manifest); err != nil {
		return nil, err
	}

	if len(manifest) != 1 {
		return nil, fmt.Errorf("no manifest found or multiple manifests found")
	}

	mainManifest := manifest[0]

	var config oci.ImageConfig

	if err := ParseJsonFromFile(filenames[mainManifest.Config], &config); err != nil {
		return nil, err
	}

	out := &fetchOciImageDefinition{}

	for _, layer := range mainManifest.Layers {
		layerDef, err := NewDefinitionFromFile(filenames[layer])
		if err != nil {
			return nil, err
		}

		readArchiveDef := NewReadArchiveBuildDefinition(layerDef, ".tar$oci.gz")

		layerFile, err := ctx.BuildChild(readArchiveDef)
		if err != nil {
			return nil, err
		}

		layerFileDigest, err := ctx.DigestFromFile(layerFile)
		if err != nil {
			return nil, err
		}

		out.LayerArchives = append(out.LayerArchives, layerFileDigest)
	}

	out.Config = config

	return out, nil
}

var (
	_ common.BuildDefinition1 = &readOciImageDefinition{}
)

func NewReadOCIImageDefinition(base common.BuildDefinition1) *readOciImageDefinition {
	return &readOciImageDefinition{
		params: ReadOciImageParameters{
			Base: base,
		},
	}
}

package builder

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/schollz/progressbar/v3"
	"github.com/tinyrange/tinyrange/pkg/builder/oci"
	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/config"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/hash"
	"go.starlark.net/starlark"
)

func init() {
	hash.RegisterType(&registryRequestDefinition{})
	hash.RegisterType(&FetchOciImageDefinition{})
}

const (
	DEFAULT_REGISTRY = "https://registry-1.docker.io/v2"
)

func ParseJsonFromFile(f filesystem.File, out any) error {
	fh, err := f.Open()
	if err != nil {
		return err
	}
	defer fh.Close()

	dec := json.NewDecoder(fh)

	if err := dec.Decode(out); err != nil {
		return err
	}

	return nil
}

type copyResponseResult struct {
	body          io.ReadCloser
	contentLength int64
	url           string
}

// WriteTo implements common.BuildResult.
func (c *copyResponseResult) WriteResult(w io.Writer) error {
	defer c.body.Close()

	prog := progressbar.DefaultBytes(c.contentLength, c.url)
	defer prog.Close()

	if _, err := io.Copy(io.MultiWriter(prog, w), c.body); err != nil {
		return err
	}

	return nil
}

var (
	_ common.BuildResult = &copyResponseResult{}
)

type ociRegistryContext struct {
	registry string
	token    string
}

func (ctx *ociRegistryContext) makeRequest(method string, url string) (*http.Request, error) {
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, err
	}

	if ctx.token != "" {
		req.Header.Set("Authorization", "Bearer "+ctx.token)
	}

	return req, nil
}

func (ctx *ociRegistryContext) responseHandler(resp *http.Response) (bool, error) {
	if resp.StatusCode == http.StatusOK {
		return true, nil
	} else if resp.StatusCode == http.StatusUnauthorized {
		// Check for a header that describes the authorization needed so we can get a new token.
		authenticate, err := oci.ParseAuthenticate(resp.Header.Get("www-authenticate"))
		if err != nil {
			return false, err
		}

		tokenUrl := fmt.Sprintf("%s?service=%s&scope=%s",
			authenticate["realm"],
			authenticate["service"],
			authenticate["scope"])

		slog.Info("registry auth", "url", tokenUrl)

		resp, err := http.Get(tokenUrl)
		if err != nil {
			return false, err
		}

		var respJson oci.TokenResponse
		decoder := json.NewDecoder(resp.Body)
		err = decoder.Decode(&respJson)
		if err != nil {
			return false, err
		}

		ctx.token = respJson.Token

		// Remake the request with the new token.
		return false, nil
	} else {
		return false, fmt.Errorf("failed to handle response code: %s", resp.Status)
	}
}

type registryRequestDefinition struct {
	ctx    *ociRegistryContext
	params RegistryRequestParameters
}

// Dependencies implements common.BuildDefinition.
func (def *registryRequestDefinition) Dependencies(ctx common.BuildContext) ([]common.DependencyNode, error) {
	return []common.DependencyNode{}, nil
}

// implements common.BuildDefinition.
func (def *registryRequestDefinition) Params() hash.SerializableValue { return def.params }
func (def *registryRequestDefinition) SerializableType() string {
	return "registryRequestDefinition"
}
func (def *registryRequestDefinition) Create(params hash.SerializableValue) hash.Definition {
	return &registryRequestDefinition{params: params.(RegistryRequestParameters)}
}

// ToStarlark implements common.BuildDefinition.
func (r *registryRequestDefinition) ToStarlark(ctx common.BuildContext, result filesystem.File) (starlark.Value, error) {
	panic("unimplemented")
}

// Build implements common.BuildDefinition.
func (r *registryRequestDefinition) Build(ctx common.BuildContext) (common.BuildResult, error) {
	req, err := r.ctx.makeRequest("GET", r.ctx.registry+r.params.Url)
	if err != nil {
		return nil, err
	}

	for _, val := range r.params.Accept {
		req.Header.Add("Accept", val)
	}

	client, err := ctx.Database().HttpClient()
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	ok, err := r.ctx.responseHandler(resp)
	if err != nil {
		return nil, err
	}
	if !ok {
		return r.Build(ctx)
	}

	return &copyResponseResult{
		body:          resp.Body,
		contentLength: resp.ContentLength,
		url:           r.ctx.registry + r.params.Url,
	}, nil
}

// NeedsBuild implements common.BuildDefinition.
func (r *registryRequestDefinition) NeedsBuild(ctx common.BuildContext, cacheTime time.Time) (bool, error) {
	if r.params.ExpireTime > 0 {
		return cacheTime.After(time.Now().Add(time.Duration(r.params.ExpireTime))), nil
	} else {
		return false, nil
	}
}

// Tag implements common.BuildDefinition.
func (r *registryRequestDefinition) Tag() string {
	tag := []string{"ociRegistryRequest", r.ctx.registry, r.params.Url}
	tag = append(tag, r.params.Accept...)
	return strings.Join(tag, "_")
}

var (
	_ common.BuildDefinition = &registryRequestDefinition{}
)

type FetchOciImageDefinition struct {
	params FetchOciImageParameters

	LayerArchives []*filesystem.FileDigest
	Config        oci.ImageConfig
}

// Dependencies implements common.Directive.
func (def *FetchOciImageDefinition) Dependencies(ctx common.BuildContext) ([]common.DependencyNode, error) {
	// The requests are dynamic dependencies.

	return []common.DependencyNode{}, nil
}

// implements common.BuildDefinition.
func (def *FetchOciImageDefinition) Params() hash.SerializableValue { return def.params }
func (def *FetchOciImageDefinition) SerializableType() string {
	return "FetchOciImageDefinition"
}
func (def *FetchOciImageDefinition) Create(params hash.SerializableValue) hash.Definition {
	return &FetchOciImageDefinition{params: params.(FetchOciImageParameters)}
}

// AsFragments implements common.Directive.
func (def *FetchOciImageDefinition) AsFragments(ctx common.BuildContext) ([]config.Fragment, error) {
	res, err := ctx.BuildChild(def)
	if err != nil {
		return nil, err
	}

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
func (def *FetchOciImageDefinition) ToStarlark(ctx common.BuildContext, result filesystem.File) (starlark.Value, error) {
	if err := ParseJsonFromFile(result, &def); err != nil {
		return nil, err
	}

	fs := filesystem.NewMemoryDirectory()

	for _, layer := range def.LayerArchives {
		layerFile, err := ctx.FileFromDigest(layer)
		if err != nil {
			return nil, err
		}

		ark, err := filesystem.ReadArchiveFromFile(layerFile)
		if err != nil {
			return starlark.None, err
		}

		if err := filesystem.ExtractArchive(ark, fs); err != nil {
			return starlark.None, err
		}
	}

	return filesystem.NewStarDirectory(fs, ""), nil
}

// tagDirective implements common.Directive.
func (def *FetchOciImageDefinition) TagDirective() { panic("unimplemented") }

func (def *FetchOciImageDefinition) FromDirective() string {
	return fmt.Sprintf("%s:%s", def.params.Image, def.params.Tag)
}

func (def *FetchOciImageDefinition) setDefaults() {
	if def.params.Registry == "" {
		def.params.Registry = DEFAULT_REGISTRY
	}
	if def.params.Tag == "" {
		def.params.Tag = "latest"
	}
	if def.params.Architecture == "" {
		def.params.Architecture = "amd64"
	}
}

func (def *FetchOciImageDefinition) indexDef(regCtx *ociRegistryContext) common.BuildDefinition {
	return &registryRequestDefinition{
		ctx: regCtx,
		params: RegistryRequestParameters{
			Url: fmt.Sprintf("/%s/manifests/%s", def.params.Image, def.params.Tag),
			Accept: []string{
				"application/vnd.docker.distribution.manifest.list.v2+json",
				"application/vnd.oci.image.index.v1+json",
			},
			ExpireTime: int64(24 * time.Hour), // Expire the tag after 24 hours.
		},
	}
}

func (def *FetchOciImageDefinition) buildFromV1Index(ctx common.BuildContext, regCtx *ociRegistryContext, index oci.ImageIndexV1) (common.BuildResult, error) {
	// Request all the layers.
	for _, layer := range index.FsLayers {
		layerArchive, err := ctx.BuildChild(
			NewReadArchiveBuildDefinition(&registryRequestDefinition{
				ctx: regCtx,
				params: RegistryRequestParameters{
					Url: fmt.Sprintf("/%s/blobs/%s", def.params.Image, layer.BlobSum),
				},
			}, ".tar.gz"),
		)
		if err != nil {
			return nil, err
		}

		// Only persist the file digests.
		// These can be used to reopen the file without requiring the entire def to be rebuilt.
		layerDigest := layerArchive.Digest()
		if layerDigest == nil {
			return nil, fmt.Errorf("%T does not support digests", layerArchive)
		}

		def.LayerArchives = append(def.LayerArchives, layerDigest)
	}

	return def, nil

}

func (def *FetchOciImageDefinition) buildFromManifest(
	ctx common.BuildContext,
	regCtx *ociRegistryContext,
	manifest oci.ImageManifest,
	config oci.ImageConfig,
) (common.BuildResult, error) {
	// Request all the layers.
	for _, layer := range manifest.Layers {
		layerArchive, err := ctx.BuildChild(
			NewReadArchiveBuildDefinition(&registryRequestDefinition{
				ctx: regCtx,
				params: RegistryRequestParameters{
					Url: fmt.Sprintf("/%s/blobs/%s", def.params.Image, layer.Digest),
				},
			}, ".tar.gz"),
		)
		if err != nil {
			return nil, err
		}

		// Only persist the file digests.
		// These can be used to reopen the file without requiring the entire def to be rebuilt.
		layerDigest := layerArchive.Digest()
		if layerDigest == nil {
			return nil, fmt.Errorf("%T does not support digests", layerArchive)
		}

		def.LayerArchives = append(def.LayerArchives, layerDigest)
	}

	def.Config = config

	return def, nil
}

func (def *FetchOciImageDefinition) buildFromIndex(ctx common.BuildContext, regCtx *ociRegistryContext, index oci.ImageIndexV2) (common.BuildResult, error) {
	// Get the right manifest for the architecture.
	var manifestId oci.ImageManifestIdentifier
	for _, manifest := range index.Manifests {
		if manifest.Platform.Architecture == def.params.Architecture {
			manifestId = manifest
		}
	}

	manifestFile, err := ctx.BuildChild(&registryRequestDefinition{
		ctx: regCtx,
		params: RegistryRequestParameters{
			Url: fmt.Sprintf("/%s/manifests/%s", def.params.Image, manifestId.Digest),
			Accept: []string{
				"application/vnd.oci.image.manifest.v1+json",
			},
		},
		// manifests are content addressed so don't expire.
	})
	if err != nil {
		return nil, err
	}

	var manifest oci.ImageManifest
	if err := ParseJsonFromFile(manifestFile, &manifest); err != nil {
		return nil, err
	}

	configFile, err := ctx.BuildChild(&registryRequestDefinition{
		ctx: regCtx,
		params: RegistryRequestParameters{
			Url: fmt.Sprintf("/%s/blobs/%s", def.params.Image, manifest.Config.Digest),
		},
		// configs are content addressed so don't expire.
	})
	if err != nil {
		return nil, err
	}

	var config oci.ImageConfig
	if err := ParseJsonFromFile(configFile, &config); err != nil {
		return nil, err
	}

	switch manifest.MediaType {
	case "application/vnd.docker.distribution.manifest.v2+json":
		return def.buildFromManifest(ctx, regCtx, manifest, config)
	case "application/vnd.oci.image.manifest.v1+json":
		return def.buildFromManifest(ctx, regCtx, manifest, config)
	default:
		return nil, fmt.Errorf("unknown manifest media type: %s", manifest.MediaType)
	}
}

// Build implements common.BuildDefinition.
func (def *FetchOciImageDefinition) Build(ctx common.BuildContext) (common.BuildResult, error) {
	regCtx := &ociRegistryContext{registry: def.params.Registry}

	// Get the index for the image tag.
	indexFile, err := ctx.BuildChild(def.indexDef(regCtx))
	if err != nil {
		return nil, err
	}

	var index oci.ImageIndexV2
	if err := ParseJsonFromFile(indexFile, &index); err != nil {
		return nil, err
	}

	switch index.MediaType {
	case "application/vnd.docker.distribution.manifest.list.v2+json":
		return def.buildFromIndex(ctx, regCtx, index)
	case "application/vnd.oci.image.index.v1+json":
		return def.buildFromIndex(ctx, regCtx, index)
	case "":
		if index.SchemaVersion != 1 {
			return nil, fmt.Errorf("index.SchemaVersion != 1 ")
		}

		var index1 oci.ImageIndexV1
		if err := ParseJsonFromFile(indexFile, &index1); err != nil {
			return nil, err
		}

		if index1.Architecture != def.params.Architecture {
			return nil, fmt.Errorf("index is of the wrong architecture: %s != %s", index1.Architecture, def.params.Architecture)
		}

		return def.buildFromV1Index(ctx, regCtx, index1)
	default:
		return nil, fmt.Errorf("unknown index media type: %s", index.MediaType)
	}
}

// NeedsBuild implements common.BuildDefinition.
func (def *FetchOciImageDefinition) NeedsBuild(ctx common.BuildContext, cacheTime time.Time) (bool, error) {
	if ctx.Database().ShouldRebuildUserDefinitions() {
		return true, nil
	}

	return ctx.NeedsBuild(def.indexDef(&ociRegistryContext{registry: def.params.Registry}))
}

// Tag implements common.BuildDefinition.
func (def *FetchOciImageDefinition) Tag() string {
	tag := []string{"fetchOciImage", def.params.Registry, def.params.Image, def.params.Tag, def.params.Architecture}

	return strings.Join(tag, "_")
}

// WriteTo implements common.BuildResult.
func (def *FetchOciImageDefinition) WriteResult(w io.Writer) error {
	enc := json.NewEncoder(w)

	if err := enc.Encode(&def); err != nil {
		return err
	}

	return nil
}

func (def *FetchOciImageDefinition) String() string { return def.Tag() }
func (*FetchOciImageDefinition) Type() string       { return "FetchOciImageDefinition" }
func (*FetchOciImageDefinition) Hash() (uint32, error) {
	return 0, fmt.Errorf("fetchOciImageDefinition is not hashable")
}
func (*FetchOciImageDefinition) Truth() starlark.Bool { return starlark.True }
func (*FetchOciImageDefinition) Freeze()              {}

var (
	_ starlark.Value         = &FetchOciImageDefinition{}
	_ common.BuildDefinition = &FetchOciImageDefinition{}
	_ common.BuildResult     = &FetchOciImageDefinition{}
	_ common.Directive       = &FetchOciImageDefinition{}
)

func NewFetchOCIImageDefinition(registry, image, tag, architecture string) *FetchOciImageDefinition {
	ret := &FetchOciImageDefinition{
		params: FetchOciImageParameters{
			Registry:     registry,
			Image:        image,
			Tag:          tag,
			Architecture: architecture,
		},
	}

	ret.setDefaults()

	return ret
}

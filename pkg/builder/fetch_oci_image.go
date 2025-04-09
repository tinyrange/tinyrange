package builder

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/schollz/progressbar/v3"
	"github.com/tinyrange/tinyrange/pkg/archive"
	"github.com/tinyrange/tinyrange/pkg/builder/oci"
	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/config"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/filesystem/star"
	"github.com/tinyrange/tinyrange/pkg/hash"
	"github.com/tinyrange/tinyrange/pkg/log"
	"go.starlark.net/starlark"
)

func init() {
	hash.RegisterType(&registryRequestDefinition{})
	hash.RegisterType(&fetchOciImageDefinition{})
	hash.RegisterType(&fetchOciImageDefinitionV2{})
}

const (
	DEFAULT_REGISTRY = "https://registry-1.docker.io/v2"
)

func ToOciArchitecture(arch config.CPUArchitecture) (string, error) {
	switch arch {
	case config.ArchX8664:
		return "amd64", nil
	case config.ArchARM64:
		return "arm64", nil
	case config.ArchInvalid:
		return ToOciArchitecture(config.HostArchitecture)
	default:
		return "", fmt.Errorf("unsupported architecture: %s", arch)
	}
}

func ParseOciImage(ociImage string) (registry string, image string, tag string, err error) {
	var ok bool

	image, tag, ok = strings.Cut(ociImage, ":")
	if !ok {
		tag = "latest"
	}

	if strings.Contains(image, ".") {
		registry, image, ok = strings.Cut(image, "/")
		if !ok {
			return "", "", "", fmt.Errorf("invalid OCI image format %s", ociImage)
		}
	}

	if registry == "" {
		registry = DEFAULT_REGISTRY
	}

	if registry == "docker.io" {
		registry = DEFAULT_REGISTRY
	}

	if !strings.HasPrefix(registry, "http://") && !strings.HasPrefix(registry, "https://") {
		registry = "https://" + registry
	}

	if registry == DEFAULT_REGISTRY && !strings.Contains(image, "/") {
		image = "library/" + image
	}

	log.Debug("parsed OCI image", "registry", registry, "image", image, "tag", tag)

	return
}

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

		log.Debug("registry auth", "url", tokenUrl)

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
		return false, fmt.Errorf("failed to handle response code %s: %s", resp.Request.URL.String(), resp.Status)
	}
}

type registryRequestDefinition struct {
	ctx    *ociRegistryContext
	params RegistryRequestParameters
}

// Dependencies implements common.BuildDefinition.
func (def *registryRequestDefinition) Dependencies() ([]common.BuildDefinition, error) {
	return nil, nil
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
func (r *registryRequestDefinition) ToStarlark(artifact common.BuildArtifact) (starlark.Value, error) {
	panic("unimplemented")
}

// Build implements common.BuildDefinition.
func (r *registryRequestDefinition) Build(ctx common.BuildContext) error {
	req, err := r.ctx.makeRequest("GET", r.ctx.registry+r.params.Url)
	if err != nil {
		return err
	}

	for _, val := range r.params.Accept {
		req.Header.Add("Accept", val)
	}

	client, err := ctx.Database().HttpClient()
	if err != nil {
		return err
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}

	ok, err := r.ctx.responseHandler(resp)
	if err != nil {
		return err
	}
	if !ok {
		defer resp.Body.Close()
		content, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		log.Debug("registry request failed", "url", r.ctx.registry+r.params.Url, "content", string(content))

		return r.Build(ctx)
	}

	return ctx.WriteDefault(&copyResponseResult{
		body:          resp.Body,
		contentLength: resp.ContentLength,
		url:           r.ctx.registry + r.params.Url,
	})
}

// NeedsBuild implements common.BuildDefinition.
func (r *registryRequestDefinition) NeedsBuild(ctx common.BuildContext) (bool, error) {
	if r.params.ExpireTime > 0 {
		return ctx.LastBuild().After(time.Now().Add(time.Duration(r.params.ExpireTime))), nil
	} else {
		return false, nil
	}
}

// String implements common.BuildDefinition.
func (r *registryRequestDefinition) String() string {
	if r.ctx == nil {
		return "registryRequestDefinition"
	}
	tag := []string{"ociRegistryRequest", r.ctx.registry, r.params.Url}
	tag = append(tag, r.params.Accept...)
	return strings.Join(tag, "_")
}

var (
	_ common.BuildDefinition = &registryRequestDefinition{}
)

type ociFetcher struct {
	ctx    common.BuildContext
	regCtx *ociRegistryContext
	params FetchOciImageParameters

	UseArchive2   bool
	LayerHashes   []hash.Hash
	LayerArchives []config.DatabaseReference
	Config        oci.ImageConfig
}

// WriteTo implements common.BuildResult.
func (def *ociFetcher) WriteResult(w io.Writer) error {
	enc := json.NewEncoder(w)

	if err := enc.Encode(&def); err != nil {
		return err
	}

	return nil
}

func (def *ociFetcher) buildFromV1Index(index oci.ImageIndexV1) error {
	// Request all the layers.
	for _, layer := range index.FsLayers {
		if def.UseArchive2 {
			layerArtifact, err := def.ctx.BuildChild(
				newReadArchive2BuildDefinition(&registryRequestDefinition{
					ctx: def.regCtx,
					params: RegistryRequestParameters{
						Url: fmt.Sprintf("/%s/blobs/%s", def.params.Image, layer.BlobSum),
					},
				}, ".tar.gz", 0),
			)
			if err != nil {
				return err
			}

			def.LayerHashes = append(def.LayerHashes, layerArtifact.DefinitionHash())
		} else {
			layerArtifact, err := def.ctx.BuildChild(
				newReadArchiveBuildDefinition(&registryRequestDefinition{
					ctx: def.regCtx,
					params: RegistryRequestParameters{
						Url: fmt.Sprintf("/%s/blobs/%s", def.params.Image, layer.BlobSum),
					},
				}, ".tar.gz", 0),
			)
			if err != nil {
				return err
			}

			layerArchive, err := layerArtifact.ReferenceForDefault()
			if err != nil {
				return err
			}

			def.LayerArchives = append(def.LayerArchives, layerArchive)
		}
	}

	return def.ctx.WriteDefault(def)
}

func (def *ociFetcher) buildFromManifest(manifest oci.ImageManifest, config oci.ImageConfig) error {
	// Request all the layers.
	for _, layer := range manifest.Layers {
		if def.UseArchive2 {
			layerArtifact, err := def.ctx.BuildChild(
				newReadArchive2BuildDefinition(&registryRequestDefinition{
					ctx: def.regCtx,
					params: RegistryRequestParameters{
						Url: fmt.Sprintf("/%s/blobs/%s", def.params.Image, layer.Digest),
					},
				}, ".tar$oci.gz", 0),
			)
			if err != nil {
				return err
			}

			def.LayerHashes = append(def.LayerHashes, layerArtifact.DefinitionHash())
		} else {
			layerArtifact, err := def.ctx.BuildChild(
				newReadArchiveBuildDefinition(&registryRequestDefinition{
					ctx: def.regCtx,
					params: RegistryRequestParameters{
						Url: fmt.Sprintf("/%s/blobs/%s", def.params.Image, layer.Digest),
					},
				}, ".tar$oci.gz", 0),
			)
			if err != nil {
				return err
			}

			layerArchive, err := layerArtifact.ReferenceForDefault()
			if err != nil {
				return err
			}

			def.LayerArchives = append(def.LayerArchives, layerArchive)
		}
	}

	def.Config = config

	return def.ctx.WriteDefault(def)
}

func (def *ociFetcher) buildFromManifestFile(manifestFile filesystem.File) error {
	var manifest oci.ImageManifest
	if err := ParseJsonFromFile(manifestFile, &manifest); err != nil {
		return err
	}

	configArtifact, err := def.ctx.BuildChild(&registryRequestDefinition{
		ctx: def.regCtx,
		params: RegistryRequestParameters{
			Url: fmt.Sprintf("/%s/blobs/%s", def.params.Image, manifest.Config.Digest),
		},
		// configs are content addressed so don't expire.
	})
	if err != nil {
		return err
	}

	configFile, err := configArtifact.Default()
	if err != nil {
		return err
	}

	var config oci.ImageConfig
	if err := ParseJsonFromFile(configFile, &config); err != nil {
		return err
	}

	switch manifest.MediaType {
	case "application/vnd.docker.distribution.manifest.v2+json":
		return def.buildFromManifest(manifest, config)
	case "application/vnd.oci.image.manifest.v1+json":
		return def.buildFromManifest(manifest, config)
	default:
		return fmt.Errorf("unknown manifest media type: %s", manifest.MediaType)
	}
}

func (def *ociFetcher) buildFromIndex(index oci.ImageIndexV2) error {
	// Get the right manifest for the architecture.
	var manifestId oci.ImageManifestIdentifier
	for _, manifest := range index.Manifests {
		if manifest.Platform.Architecture == def.params.Architecture {
			manifestId = manifest
		}
	}

	manifestArtifact, err := def.ctx.BuildChild(&registryRequestDefinition{
		ctx: def.regCtx,
		params: RegistryRequestParameters{
			Url: fmt.Sprintf("/%s/manifests/%s", def.params.Image, manifestId.Digest),
			Accept: []string{
				"application/vnd.oci.image.manifest.v1+json",
			},
		},
		// manifests are content addressed so don't expire.
	})
	if err != nil {
		return err
	}

	manifestFile, err := manifestArtifact.Default()
	if err != nil {
		return err
	}

	return def.buildFromManifestFile(manifestFile)
}

// Build implements common.BuildDefinition.
func (def *ociFetcher) buildTop() error {
	regCtx := &ociRegistryContext{registry: def.params.Registry}

	indexDef := &registryRequestDefinition{
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

	// Get the index for the image tag.
	indexArtifact, err := def.ctx.BuildChild(indexDef)
	if err != nil {
		return err
	}

	indexFile, err := indexArtifact.Default()
	if err != nil {
		return err
	}

	var index oci.ImageIndexV2
	if err := ParseJsonFromFile(indexFile, &index); err != nil {
		return err
	}

	switch index.MediaType {
	case "application/vnd.docker.distribution.manifest.list.v2+json":
		return def.buildFromIndex(index)
	case "application/vnd.docker.distribution.manifest.v2+json":
		return def.buildFromManifestFile(indexFile)
	case "application/vnd.oci.image.index.v1+json":
		return def.buildFromIndex(index)
	case "":
		if index.SchemaVersion != 1 {
			return fmt.Errorf("index.SchemaVersion != 1 ")
		}

		var index1 oci.ImageIndexV1
		if err := ParseJsonFromFile(indexFile, &index1); err != nil {
			return err
		}

		if index1.Architecture != def.params.Architecture {
			return fmt.Errorf("index is of the wrong architecture: %s != %s", index1.Architecture, def.params.Architecture)
		}

		return def.buildFromV1Index(index1)
	default:
		return fmt.Errorf("unknown index media type: %s", index.MediaType)
	}
}

func (def *ociFetcher) toStarlark(artifact common.BuildArtifact) (starlark.Value, error) {
	fs := filesystem.NewMemoryDirectory()

	for _, layer := range def.LayerArchives {
		layerFile, err := artifact.Database().Builder().FileFromReference(layer)
		if err != nil {
			return nil, err
		}

		if def.UseArchive2 {
			return nil, fmt.Errorf("archive2 not implemented")
		} else {
			ark, err := archive.ReadArchiveFromFile(layerFile)
			if err != nil {
				return starlark.None, err
			}

			if err := archive.ExtractArchive(ark, fs); err != nil {
				return starlark.None, err
			}
		}
	}

	return star.NewStarDirectory(fs, ""), nil
}

func (def *ociFetcher) asFragments(ctx common.BuildContext) ([]config.Fragment, error) {
	var ret []config.Fragment

	if def.UseArchive2 {
		for _, layer := range def.LayerHashes {
			def, err := ctx.Database().Builder().GetDefinitionByHash(layer)
			if err != nil {
				return nil, fmt.Errorf("failed to get definition by hash: %w", err)
			}

			readArchive2, ok := def.(*readArchive2BuildDefinition)
			if !ok {
				return nil, fmt.Errorf("layer is not a readArchive2BuildDefinition: %T", def)
			}

			fragments, err := readArchive2.AsFragments(ctx, common.SpecialDirectiveHandlers{})
			if err != nil {
				return nil, err
			}

			ret = append(ret, fragments...)
		}
	} else {
		for _, archive := range def.LayerArchives {
			ret = append(ret, config.Fragment{Archive: &config.ArchiveFragment{DatabaseReference: archive}})
		}
	}

	slices.Reverse(ret)

	if def.Config.Config.Env != nil {
		ret = append(ret, config.Fragment{Environment: &config.EnvironmentFragment{Variables: def.Config.Config.Env}})
	}

	return ret, nil
}

type fetchOciImageDefinition struct {
	params FetchOciImageParameters
}

// Dependencies implements common.BuildDefinition.
func (def *fetchOciImageDefinition) Dependencies() ([]common.BuildDefinition, error) {
	return nil, nil
}

// implements common.BuildDefinition.
func (def *fetchOciImageDefinition) Params() hash.SerializableValue { return def.params }
func (def *fetchOciImageDefinition) SerializableType() string {
	return "FetchOciImageDefinition_v0"
}
func (def *fetchOciImageDefinition) Create(params hash.SerializableValue) hash.Definition {
	return &fetchOciImageDefinition{params: params.(FetchOciImageParameters)}
}

func (def *fetchOciImageDefinition) setDefaults() {
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

// AsFragments implements common.Directive.
func (def *fetchOciImageDefinition) AsFragments(ctx common.BuildContext, special common.SpecialDirectiveHandlers) ([]config.Fragment, error) {
	art, err := ctx.BuildChild(def)
	if err != nil {
		return nil, err
	}

	res, err := art.Default()
	if err != nil {
		return nil, err
	}

	var fetcher ociFetcher

	if err := ParseJsonFromFile(res, &fetcher); err != nil {
		return nil, err
	}

	return fetcher.asFragments(ctx)
}

// ToStarlark implements common.BuildDefinition.
func (def *fetchOciImageDefinition) ToStarlark(artifact common.BuildArtifact) (starlark.Value, error) {
	result, err := artifact.Default()
	if err != nil {
		return nil, err
	}

	var fetcher ociFetcher

	if err := ParseJsonFromFile(result, &fetcher); err != nil {
		return nil, err
	}

	return fetcher.toStarlark(artifact)
}

// NeedsBuild implements common.BuildDefinition.
func (def *fetchOciImageDefinition) NeedsBuild(ctx common.BuildContext) (bool, error) {
	return ctx.ShouldRebuildUserDefinitions(), nil
}

// Tag implements common.BuildDefinition.
func (def *fetchOciImageDefinition) Tag() string {
	tag := []string{"fetchOciImage", def.params.Registry, def.params.Image, def.params.Tag, def.params.Architecture}

	return strings.Join(tag, "_")
}

// Build implements common.BuildDefinition.
func (def *fetchOciImageDefinition) Build(ctx common.BuildContext) error {
	fetcher := &ociFetcher{
		ctx:    ctx,
		regCtx: &ociRegistryContext{registry: def.params.Registry},
		params: def.params,
	}

	return fetcher.buildTop()
}

func (def *fetchOciImageDefinition) String() string { return def.Tag() }
func (*fetchOciImageDefinition) Type() string       { return "FetchOciImageDefinition" }
func (*fetchOciImageDefinition) Hash() (uint32, error) {
	return 0, fmt.Errorf("fetchOciImageDefinition is not hashable")
}
func (*fetchOciImageDefinition) Truth() starlark.Bool { return starlark.True }
func (*fetchOciImageDefinition) Freeze()              {}

var (
	_ starlark.Value         = &fetchOciImageDefinition{}
	_ common.BuildDefinition = &fetchOciImageDefinition{}
	_ common.Directive       = &fetchOciImageDefinition{}
)

type fetchOciImageDefinitionV2 struct {
	params FetchOciImageParameters
}

// Dependencies implements common.BuildDefinition.
func (def *fetchOciImageDefinitionV2) Dependencies() ([]common.BuildDefinition, error) {
	return nil, nil
}

// implements common.BuildDefinition.
func (def *fetchOciImageDefinitionV2) Params() hash.SerializableValue { return def.params }
func (def *fetchOciImageDefinitionV2) SerializableType() string {
	return "FetchOciImageDefinition_v2"
}
func (def *fetchOciImageDefinitionV2) Create(params hash.SerializableValue) hash.Definition {
	return &fetchOciImageDefinitionV2{params: params.(FetchOciImageParameters)}
}

func (def *fetchOciImageDefinitionV2) setDefaults() {
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

// AsFragments implements common.Directive.
func (def *fetchOciImageDefinitionV2) AsFragments(ctx common.BuildContext, special common.SpecialDirectiveHandlers) ([]config.Fragment, error) {
	art, err := ctx.BuildChild(def)
	if err != nil {
		return nil, err
	}

	res, err := art.Default()
	if err != nil {
		return nil, err
	}

	var fetcher ociFetcher

	if err := ParseJsonFromFile(res, &fetcher); err != nil {
		return nil, err
	}

	return fetcher.asFragments(ctx)
}

// ToStarlark implements common.BuildDefinition.
func (def *fetchOciImageDefinitionV2) ToStarlark(artifact common.BuildArtifact) (starlark.Value, error) {
	result, err := artifact.Default()
	if err != nil {
		return nil, err
	}

	var fetcher ociFetcher

	if err := ParseJsonFromFile(result, &fetcher); err != nil {
		return nil, err
	}

	return fetcher.toStarlark(artifact)
}

// NeedsBuild implements common.BuildDefinition.
func (def *fetchOciImageDefinitionV2) NeedsBuild(ctx common.BuildContext) (bool, error) {
	return ctx.ShouldRebuildUserDefinitions(), nil
}

// Tag implements common.BuildDefinition.
func (def *fetchOciImageDefinitionV2) Tag() string {
	tag := []string{"fetchOciImage_v2", def.params.Registry, def.params.Image, def.params.Tag, def.params.Architecture}

	return strings.Join(tag, "_")
}

// Build implements common.BuildDefinition.
func (def *fetchOciImageDefinitionV2) Build(ctx common.BuildContext) error {
	fetcher := &ociFetcher{
		ctx:    ctx,
		regCtx: &ociRegistryContext{registry: def.params.Registry},
		params: def.params,

		UseArchive2: true,
	}

	return fetcher.buildTop()
}

func (def *fetchOciImageDefinitionV2) String() string { return def.Tag() }
func (*fetchOciImageDefinitionV2) Type() string       { return "FetchOciImageDefinition_v2" }
func (*fetchOciImageDefinitionV2) Hash() (uint32, error) {
	return 0, fmt.Errorf("fetchOciImageDefinition is not hashable")
}
func (*fetchOciImageDefinitionV2) Truth() starlark.Bool { return starlark.True }
func (*fetchOciImageDefinitionV2) Freeze()              {}

var (
	_ common.FetchOciImageDefinition = &fetchOciImageDefinitionV2{}
)

func newFetchOCIImageDefinition(registry, image, tag, architecture string) common.FetchOciImageDefinition {
	ret := &fetchOciImageDefinition{
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

func newFetchOCIImageV2Definition(registry, image, tag, architecture string) common.FetchOciImageDefinition {
	ret := &fetchOciImageDefinitionV2{
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

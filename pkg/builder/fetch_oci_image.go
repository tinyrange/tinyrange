package builder

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/tinyrange/tinyrange/pkg/builder/oci"
	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"go.starlark.net/starlark"
)

const (
	DEFAULT_REGISTRY = "https://registry-1.docker.io/v2"
)

func parseJsonFromFile(f filesystem.File, out any) error {
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
	body io.ReadCloser
}

// WriteTo implements common.BuildResult.
func (c *copyResponseResult) WriteTo(w io.Writer) (n int64, err error) {
	defer c.body.Close()

	return io.Copy(w, c.body)
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
	ctx        *ociRegistryContext
	url        string
	expireTime time.Duration
	accept     []string
}

// Build implements common.BuildDefinition.
func (r *registryRequestDefinition) Build(ctx common.BuildContext) (common.BuildResult, error) {
	req, err := r.ctx.makeRequest("GET", r.ctx.registry+r.url)
	if err != nil {
		return nil, err
	}

	for _, val := range r.accept {
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

	return &copyResponseResult{body: resp.Body}, nil
}

// NeedsBuild implements common.BuildDefinition.
func (r *registryRequestDefinition) NeedsBuild(ctx common.BuildContext, cacheTime time.Time) (bool, error) {
	if r.expireTime > 0 {
		return cacheTime.After(time.Now().Add(r.expireTime)), nil
	} else {
		return false, nil
	}
}

// Tag implements common.BuildDefinition.
func (r *registryRequestDefinition) Tag() string {
	tag := []string{"ociRegistryRequest", r.ctx.registry, r.url}
	tag = append(tag, r.accept...)
	return strings.Join(tag, "_")
}

var (
	_ common.BuildDefinition = &registryRequestDefinition{}
)

type FetchOciImageDefinition struct {
	registry     string
	image        string
	tag          string
	architecture string

	LayerArchives []*filesystem.FileDigest
}

// tagDirective implements common.Directive.
func (def *FetchOciImageDefinition) TagDirective() { panic("unimplemented") }

func (def *FetchOciImageDefinition) FromDirective() string {
	return fmt.Sprintf("%s:%s", def.image, def.tag)
}

func (def *FetchOciImageDefinition) SetDefaults() {
	if def.registry == "" {
		def.registry = DEFAULT_REGISTRY
	}
	if def.tag == "" {
		def.tag = "latest"
	}
	if def.architecture == "" {
		def.architecture = "amd64"
	}
}

func (def *FetchOciImageDefinition) indexDef(regCtx *ociRegistryContext) common.BuildDefinition {
	return &registryRequestDefinition{
		ctx: regCtx,
		url: fmt.Sprintf("/%s/manifests/%s", def.image, def.tag),
		accept: []string{
			"application/vnd.docker.distribution.manifest.list.v2+json",
			"application/vnd.oci.image.index.v1+json",
		},
		expireTime: 24 * time.Hour, // Expire the tag after 24 hours.
	}
}

func (def *FetchOciImageDefinition) buildFromV1Index(ctx common.BuildContext, regCtx *ociRegistryContext, index oci.ImageIndexV1) (common.BuildResult, error) {
	// Request all the layers.
	for _, layer := range index.FsLayers {
		layerArchive, err := ctx.BuildChild(&ReadArchiveBuildDefinition{
			Base: &FetchHttpBuildDefinition{
				requestMaker:    regCtx.makeRequest,
				responseHandler: regCtx.responseHandler,
				Url:             fmt.Sprintf("%s/%s/blobs/%s", regCtx.registry, def.image, layer.BlobSum),
			},
			Kind: ".tar.gz",
		})
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

func (def *FetchOciImageDefinition) buildFromManifest(ctx common.BuildContext, regCtx *ociRegistryContext, manifest oci.ImageManifest) (common.BuildResult, error) {
	// Request all the layers.
	for _, layer := range manifest.Layers {
		layerArchive, err := ctx.BuildChild(&ReadArchiveBuildDefinition{
			Base: &FetchHttpBuildDefinition{
				requestMaker:    regCtx.makeRequest,
				responseHandler: regCtx.responseHandler,
				Url:             fmt.Sprintf("%s/%s/blobs/%s", regCtx.registry, def.image, layer.Digest),
			},
			Kind: ".tar.gz",
		})
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

func (def *FetchOciImageDefinition) buildFromIndex(ctx common.BuildContext, regCtx *ociRegistryContext, index oci.ImageIndexV2) (common.BuildResult, error) {
	// Get the right manifest for the architecture.
	var manifestId oci.ImageManifestIdentifier
	for _, manifest := range index.Manifests {
		if manifest.Platform.Architecture == def.architecture {
			manifestId = manifest
		}
	}

	manifestFile, err := ctx.BuildChild(&registryRequestDefinition{
		ctx: regCtx,
		url: fmt.Sprintf("/%s/manifests/%s", def.image, manifestId.Digest),
		accept: []string{
			"application/vnd.oci.image.manifest.v1+json",
		},
		// manifests are content addressed so don't expire.
	})
	if err != nil {
		return nil, err
	}

	var manifest oci.ImageManifest
	if err := parseJsonFromFile(manifestFile, &manifest); err != nil {
		return nil, err
	}

	switch manifest.MediaType {
	case "application/vnd.docker.distribution.manifest.v2+json":
		return def.buildFromManifest(ctx, regCtx, manifest)
	case "application/vnd.oci.image.manifest.v1+json":
		return def.buildFromManifest(ctx, regCtx, manifest)
	default:
		return nil, fmt.Errorf("unknown manifest media type: %s", manifest.MediaType)
	}
}

// Build implements common.BuildDefinition.
func (def *FetchOciImageDefinition) Build(ctx common.BuildContext) (common.BuildResult, error) {
	regCtx := &ociRegistryContext{registry: def.registry}

	// Get the index for the image tag.
	indexFile, err := ctx.BuildChild(def.indexDef(regCtx))
	if err != nil {
		return nil, err
	}

	var index oci.ImageIndexV2
	if err := parseJsonFromFile(indexFile, &index); err != nil {
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
		if err := parseJsonFromFile(indexFile, &index1); err != nil {
			return nil, err
		}

		if index1.Architecture != def.architecture {
			return nil, fmt.Errorf("index is of the wrong architecture: %s != %s", index1.Architecture, def.architecture)
		}

		return def.buildFromV1Index(ctx, regCtx, index1)
	default:
		return nil, fmt.Errorf("unknown index media type: %s", index.MediaType)
	}
}

// NeedsBuild implements common.BuildDefinition.
func (def *FetchOciImageDefinition) NeedsBuild(ctx common.BuildContext, cacheTime time.Time) (bool, error) {
	return ctx.NeedsBuild(def.indexDef(&ociRegistryContext{registry: def.registry}))
}

// Tag implements common.BuildDefinition.
func (def *FetchOciImageDefinition) Tag() string {
	tag := []string{"fetchOciImage", def.registry, def.image, def.tag, def.architecture}

	return strings.Join(tag, "_")
}

// WriteTo implements common.BuildResult.
func (def *FetchOciImageDefinition) WriteTo(w io.Writer) (n int64, err error) {
	buf := new(bytes.Buffer)

	enc := json.NewEncoder(buf)

	if err := enc.Encode(&def); err != nil {
		return 0, err
	}

	return io.Copy(w, buf)
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
		registry:     registry,
		image:        image,
		tag:          tag,
		architecture: architecture,
	}

	ret.SetDefaults()

	return ret
}

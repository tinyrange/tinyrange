package builder

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/schollz/progressbar/v3"
	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/hash"
	"go.starlark.net/starlark"
)

func init() {
	hash.RegisterType(&fetchHttpBuildDefinition{})
}

var ErrNotFound = errors.New("HTTP 404: Not Found")

type fetchHttpBuildDefinition struct {
	params FetchHttpParameters

	resp *http.Response
}

// Redistributable implements common.RedistributableDefinition.
func (def *fetchHttpBuildDefinition) Redistributable() bool {
	return true
}

// implements common.BuildDefinition.
func (def *fetchHttpBuildDefinition) Params() hash.SerializableValue { return def.params }
func (def *fetchHttpBuildDefinition) SerializableType() string       { return "FetchHttpBuildDefinition" }
func (def *fetchHttpBuildDefinition) Create(params hash.SerializableValue) hash.Definition {
	return &fetchHttpBuildDefinition{params: params.(FetchHttpParameters)}
}

// ToStarlark implements common.BuildDefinition.
func (f *fetchHttpBuildDefinition) ToStarlark(ctx common.BuildContext1, artifact common.BuildArtifact) (starlark.Value, error) {
	result, err := artifact.Default()
	if err != nil {
		return nil, err
	}

	return filesystem.NewStarFile(result, artifact.DefinitionHash().String()), nil
}

// NeedsBuild implements BuildDefinition.
func (f *fetchHttpBuildDefinition) NeedsBuild(ctx common.BuildContext1) (bool, error) {
	if f.params.ExpireTime != 0 {
		return time.Now().After(ctx.LastBuild().Add(time.Duration(f.params.ExpireTime))), nil
	}

	// The HTTP cache is never invalidated unless the client asks it to be.
	return false, nil
}

// WriteTo implements BuildResult.
func (f *fetchHttpBuildDefinition) WriteResult(w io.Writer) error {
	if f.resp == nil {
		return fmt.Errorf("FetchHttpBuildDefinition: f.resp == nil")
	}
	defer f.resp.Body.Close()

	prog := progressbar.DefaultBytes(f.resp.ContentLength, f.params.Url)
	defer prog.Close()

	if _, err := io.Copy(io.MultiWriter(prog, w), f.resp.Body); err != nil {
		return err
	}

	return nil
}

// Build implements BuildDefinition.
func (f *fetchHttpBuildDefinition) Build(ctx common.BuildContext1) (common.BuildResult, error) {
	urls, err := ctx.Database().UrlsFor(f.params.Url)
	if err != nil {
		return nil, err
	}

	client, err := ctx.Database().HttpClient()
	if err != nil {
		return nil, err
	}

	onlyNotFound := true

	for _, url := range urls {
		var req *http.Request

		req, err = http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, err
		}

		if f.params.Headers != nil {
			for k, v := range f.params.Headers {
				req.Header.Add(k, v)
			}
		}

		resp, err := client.Do(req)
		if err != nil {
			slog.Warn("failed to fetch", "url", url, "err", err)
			onlyNotFound = false
			continue
		}

		if resp.StatusCode == http.StatusOK {
			f.resp = resp

			return f, nil
		} else if resp.StatusCode == http.StatusNotFound {
			slog.Warn("failed to fetch", "url", url, "err", ErrNotFound)
			continue
		} else {
			slog.Warn("failed to fetch", "url", url, "err", fmt.Errorf("bad status: %s", resp.Status))
			onlyNotFound = false
			continue
		}

		// TODO(joshua): Check the last modified time on the server.
	}

	if onlyNotFound {
		return nil, ErrNotFound
	}

	if !ctx.LastBuild().IsZero() {
		return nil, nil
	}

	return nil, fmt.Errorf("unable to find options to fetch %s", f.params.Url)
}

func (def *fetchHttpBuildDefinition) String() string { return def.params.Url }
func (*fetchHttpBuildDefinition) Type() string       { return "FetchHttpBuildDefinition" }
func (*fetchHttpBuildDefinition) Hash() (uint32, error) {
	return 0, fmt.Errorf("FetchHttpBuildDefinition is not hashable")
}
func (*fetchHttpBuildDefinition) Truth() starlark.Bool { return starlark.True }
func (*fetchHttpBuildDefinition) Freeze()              {}

var (
	_ starlark.Value                   = &fetchHttpBuildDefinition{}
	_ common.BuildDefinition1          = &fetchHttpBuildDefinition{}
	_ common.RedistributableDefinition = &fetchHttpBuildDefinition{}
	_ common.BuildResult               = &fetchHttpBuildDefinition{}
)

func newFetchHttpBuildDefinition(url string, expireTime time.Duration, headers map[string]string) common.StarBuildDefinition1 {
	return &fetchHttpBuildDefinition{params: FetchHttpParameters{Url: url, ExpireTime: int64(expireTime), Headers: headers}}
}

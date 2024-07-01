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
	"go.starlark.net/starlark"
)

var ErrNotFound = errors.New("HTTP 404: Not Found")

type FetchHttpBuildDefinition struct {
	Url        string
	ExpireTime time.Duration

	requestMaker func(method string, url string) (*http.Request, error)
	// If this returns false, nil then the request will be redone.
	responseHandler func(resp *http.Response) (bool, error)
	resp            *http.Response
}

// NeedsBuild implements BuildDefinition.
func (f *FetchHttpBuildDefinition) NeedsBuild(ctx common.BuildContext, cacheTime time.Time) (bool, error) {
	if f.ExpireTime != 0 {
		return time.Now().After(cacheTime.Add(f.ExpireTime)), nil
	}

	// The HTTP cache is never invalidated unless the client asks it to be.
	return false, nil
}

// WriteTo implements BuildResult.
func (f *FetchHttpBuildDefinition) WriteTo(w io.Writer) (n int64, err error) {
	defer f.resp.Body.Close()

	prog := progressbar.DefaultBytes(f.resp.ContentLength, f.Url)
	defer prog.Close()

	return io.Copy(io.MultiWriter(prog, w), f.resp.Body)
}

// Build implements BuildDefinition.
func (f *FetchHttpBuildDefinition) Build(ctx common.BuildContext) (common.BuildResult, error) {
	urls, err := ctx.Database().UrlsFor(f.Url)
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

		if f.requestMaker != nil {
			req, err = f.requestMaker("GET", url)
			if err != nil {
				return nil, err
			}
		} else {
			req, err = http.NewRequest("GET", url, nil)
			if err != nil {
				return nil, err
			}
		}

		resp, err := client.Do(req)
		if err != nil {
			slog.Warn("failed to fetch", "url", url, "err", err)
			onlyNotFound = false
			continue
		}

		if f.responseHandler != nil {
			ok, err := f.responseHandler(resp)
			if err != nil {
				return nil, err
			}

			if !ok {
				return f.Build(ctx)
			}
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

	if ctx.HasCached() {
		return nil, nil
	}

	return nil, fmt.Errorf("unable to find options to fetch %s", f.Url)
}

// Tag implements BuildDefinition.
func (f *FetchHttpBuildDefinition) Tag() string {
	return f.Url
}

func (def *FetchHttpBuildDefinition) String() string { return def.Tag() }
func (*FetchHttpBuildDefinition) Type() string       { return "FetchHttpBuildDefinition" }
func (*FetchHttpBuildDefinition) Hash() (uint32, error) {
	return 0, fmt.Errorf("FetchHttpBuildDefinition is not hashable")
}
func (*FetchHttpBuildDefinition) Truth() starlark.Bool { return starlark.True }
func (*FetchHttpBuildDefinition) Freeze()              {}

var (
	_ starlark.Value         = &FetchHttpBuildDefinition{}
	_ common.BuildDefinition = &FetchHttpBuildDefinition{}
	_ common.BuildResult     = &FetchHttpBuildDefinition{}
)

func NewFetchHttpBuildDefinition(url string, expireTime time.Duration) *FetchHttpBuildDefinition {
	return &FetchHttpBuildDefinition{Url: url, ExpireTime: expireTime}
}

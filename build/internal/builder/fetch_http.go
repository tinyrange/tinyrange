package builder

import (
	"fmt"
	"io"
	"net/http"

	"github.com/tinyrange/tinyrange/build/internal/common"
	"github.com/tinyrange/tinyrange/build/internal/registry"
	"github.com/tinyrange/tinyrange/build/proto"
)

func ReaderFromFetchHttp(ctx common.Context, fetch *proto.FetchHttpDefinition) (io.ReadCloser, error) {
	client := ctx.HttpClient()

	req, err := http.NewRequest("GET", fetch.Url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, fmt.Errorf("failed to fetch http: %s", resp.Status)
	}

	return ctx.ProgressBar(fetch.Url, resp.ContentLength, resp.Body), nil
}

type fetchHttpBuilder struct {
}

// Build implements common.Builder.
func (f *fetchHttpBuilder) Build(ctx common.Context) error {
	var params proto.FetchHttpDefinition
	if err := ctx.Decode(&params); err != nil {
		return err
	}

	r, err := ReaderFromFetchHttp(ctx, &params)
	if err != nil {
		return err
	}
	defer r.Close()

	out, err := ctx.Create(common.FileType_Plain)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, r); err != nil {
		return err
	}

	return nil
}

func init() {
	registry.Register(
		common.TYPE_NAME_FETCH_HTTP,
		&proto.FetchHttpDefinition{},
		&fetchHttpBuilder{})
}

package oci

import (
	"fmt"
	"strings"

	"github.com/tinyrange/tinyrange/build"
	"github.com/tinyrange/tinyrange/vibe_party/oci/oci"
	"github.com/tinyrange/tinyrange/vibe_party/oci/proto"
)

const typeNameFetchOCI = "tinyrange/alpha/fetch_oci_image"

type fetchOciImageBuilder struct{}

// Build implements common.Builder.
func (b *fetchOciImageBuilder) Build(ctx build.Context) error {
	var params proto.FetchOciImageDefinition
	if err := ctx.Decode(&params); err != nil {
		return err
	}

	registryURL := params.Registry
	if registryURL == "" {
		registryURL = "https://registry-1.docker.io/v2"
	}
	if !strings.HasPrefix(registryURL, "http://") && !strings.HasPrefix(registryURL, "https://") {
		registryURL = "https://" + registryURL
	}
	if !strings.HasSuffix(registryURL, "/v2") {
		registryURL += "/v2"
	}

	if params.Image == "" {
		return fmt.Errorf("image is required")
	}
	ref := params.Reference
	if ref == "" {
		ref = "latest"
	}

	ark, err := ctx.CreateArchive()
	if err != nil {
		return err
	}
	defer ark.Close()

	client := &oci.Client{HTTPClient: ctx.HttpClient(), Registry: registryURL}
	return client.FetchImage(params.Image, ref, params.Architecture, ark)
}

func init() {
	build.RegisterBuilder(typeNameFetchOCI, &proto.FetchOciImageDefinition{}, &fetchOciImageBuilder{})
}

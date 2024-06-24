package oci

import (
	"archive/tar"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/filesystem/ext4"
)

const (
	DEFAULT_REGISTRY = "https://registry-1.docker.io/v2"
)

type imagePlatform struct {
	Architecture string `json:"architecture"`
	Os           string `json:"os"`
	Variant      string `json:"variant"`
}

type ImageManifestIdentifier struct {
	MediaType string        `json:"mediaType"`
	Size      uint64        `json:"size"`
	Digest    string        `json:"digest"`
	Platform  imagePlatform `json:"platform"`
}

type ImageIndexV2 struct {
	SchemaVersion int                       `json:"schemaVersion"`
	MediaType     string                    `json:"mediaType"`
	Manifests     []ImageManifestIdentifier `json:"manifests"`
}

type tokenResponse struct {
	Token       string `json:"token"`
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	IssuedAt    string `json:"issued_at"`
}

type imageConfigIdentifier struct {
	MediaType string `json:"mediaType"`
	Size      uint64 `json:"size"`
	Digest    string `json:"digest"`
}

type ImageLayerIdentifier struct {
	MediaType string `json:"mediaType"`
	Size      uint64 `json:"size"`
	Digest    string `json:"digest"`
}

type ImageManifest struct {
	SchemaVersion int                    `json:"schemaVersion"`
	MediaType     string                 `json:"mediaType"`
	Config        imageConfigIdentifier  `json:"config"`
	Layers        []ImageLayerIdentifier `json:"layers"`
}

func parseAuthenticate(value string) (map[string]string, error) {
	if value == "" {
		return nil, fmt.Errorf("failed to find authenticate header, can't automatically authenticate")
	}

	value = strings.TrimPrefix(value, "Bearer ")

	tokens := strings.Split(value, ",")

	ret := make(map[string]string)

	for _, token := range tokens {
		key, value, ok := strings.Cut(token, "=")
		if !ok {
			return nil, fmt.Errorf("malformed header value")
		}
		value = strings.Trim(value, "\"")
		ret[key] = value
	}

	return ret, nil
}

type OciImageDownloader struct {
	token string
}

func (dl *OciImageDownloader) makeRegistryRequest(method string, url string, acceptHeaders []string) (*http.Response, error) {
	slog.Info("making registry request", "method", method, "url", url, "accept", acceptHeaders)

	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, err
	}

	if dl.token != "" {
		req.Header.Set("Authorization", "Bearer "+dl.token)
	}

	for _, val := range acceptHeaders {
		req.Header.Add("Accept", val)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == http.StatusOK {
		return resp, nil
	} else if resp.StatusCode == http.StatusUnauthorized {
		// Check for a header that describes the authorization needed so we can get a new token.
		authenticate, err := parseAuthenticate(resp.Header.Get("www-authenticate"))
		if err != nil {
			return nil, err
		}

		tokenUrl := fmt.Sprintf("%s?service=%s&scope=%s",
			authenticate["realm"],
			authenticate["service"],
			authenticate["scope"])

		slog.Info("registry auth", "url", tokenUrl)

		resp, err := http.Get(tokenUrl)
		if err != nil {
			return nil, err
		}

		var respJson tokenResponse
		decoder := json.NewDecoder(resp.Body)
		err = decoder.Decode(&respJson)
		if err != nil {
			return nil, err
		}

		dl.token = respJson.Token

		// Remake the request with the new token.
		return dl.makeRegistryRequest(method, url, acceptHeaders)
	} else {
		return nil, fmt.Errorf("failed to handle response code: %s", resp.Status)
	}
}

func (dl *OciImageDownloader) downloadJson(method string, url string, acceptHeaders []string, out any) error {
	resp, err := dl.makeRegistryRequest(method, url, acceptHeaders)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	dec := json.NewDecoder(resp.Body)

	if err := dec.Decode(out); err != nil {
		return err
	}

	return nil
}

func (dl *OciImageDownloader) ExtractOciImage(fs *ext4.Ext4Filesystem, name string) error {
	imageName, ref, _ := strings.Cut(name, ":")

	if imageName == "library/scratch" {
		return nil
	}

	// download image index.
	indexUrl := fmt.Sprintf("%s/%s/manifests/%s", DEFAULT_REGISTRY, imageName, ref)
	var index ImageIndexV2
	if err := dl.downloadJson("GET", indexUrl, []string{
		"application/vnd.docker.distribution.manifest.list.v2+json",
		"application/vnd.oci.image.index.v1+json",
	}, &index); err != nil {
		return err
	}

	if index.SchemaVersion == 2 {
		var manifestId ImageManifestIdentifier
		for _, manifest := range index.Manifests {
			if manifest.Platform.Architecture == "amd64" {
				manifestId = manifest
			}
		}

		manifestUrl := fmt.Sprintf("%s/%s/manifests/%s", DEFAULT_REGISTRY, imageName, manifestId.Digest)
		var manifest ImageManifest
		if err := dl.downloadJson("GET", manifestUrl, []string{
			"application/vnd.oci.image.manifest.v1+json",
		}, &manifest); err != nil {
			return err
		}

		layers := manifest.Layers
		slices.Reverse(layers)

		for _, layer := range layers {
			layerUrl := fmt.Sprintf("%s/%s/blobs/%s", DEFAULT_REGISTRY, imageName, layer.Digest)
			resp, err := dl.makeRegistryRequest("GET", layerUrl, []string{})
			if err != nil {
				return err
			}

			// assume tar.gz
			if err := filesystem.ExtractReaderTo(resp.Body, ".tar.gz", fs, func(hdr *tar.Header) bool {
				if !strings.HasPrefix(hdr.Name, "/") {
					hdr.Name = "/" + hdr.Name
				}
				if hdr.Typeflag == tar.TypeLink && !strings.HasPrefix(hdr.Linkname, "/") {
					hdr.Linkname = "/" + hdr.Linkname
				}

				return true
			}); err != nil {
				return err
			}
		}
	} else {
		return fmt.Errorf("expected schema version 2 got: %+v", index)
	}

	// return fmt.Errorf("not implemented")
	return nil
}

func NewDownloader() *OciImageDownloader {
	return &OciImageDownloader{}
}

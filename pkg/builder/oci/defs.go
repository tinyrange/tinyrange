package oci

import (
	"fmt"
	"strings"
	"time"
)

type ImagePlatform struct {
	Architecture string `json:"architecture"`
	Os           string `json:"os"`
	Variant      string `json:"variant"`
}

type ImageManifestIdentifier struct {
	MediaType string        `json:"mediaType"`
	Size      uint64        `json:"size"`
	Digest    string        `json:"digest"`
	Platform  ImagePlatform `json:"platform"`
}

type ImageIndexV2 struct {
	SchemaVersion int                       `json:"schemaVersion"`
	MediaType     string                    `json:"mediaType"`
	Manifests     []ImageManifestIdentifier `json:"manifests"`
}

type TokenResponse struct {
	Token       string `json:"token"`
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	IssuedAt    string `json:"issued_at"`
}

type ImageConfigIdentifier struct {
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
	Config        ImageConfigIdentifier  `json:"config"`
	Layers        []ImageLayerIdentifier `json:"layers"`
}

type ImageLayerV1 struct {
	BlobSum string `json:"blobSum"`
}

type ImageIndexV1 struct {
	SchemaVersion int            `json:"schemaVersion"`
	Name          string         `json:"name"`
	Tag           string         `json:"tag"`
	Architecture  string         `json:"architecture"`
	FsLayers      []ImageLayerV1 `json:"fsLayers"`
}

type ImageConfigHistory struct {
	Created    time.Time `json:"created"`
	CreatedBy  string    `json:"created_by"`
	Comment    string    `json:"comment"`
	EmptyLayer bool      `json:"empty_layer"`
}

type ImageConfigInfo struct {
	Hostname     string            `json:"Hostname"`
	Domainname   string            `json:"Domainname"`
	User         string            `json:"User"`
	AttachStdin  bool              `json:"AttachStdin"`
	AttachStdout bool              `json:"AttachStdout"`
	AttachStderr bool              `json:"AttachStderr"`
	Tty          bool              `json:"Tty"`
	OpenStdin    bool              `json:"OpenStdin"`
	StdinOnce    bool              `json:"StdinOnce"`
	Env          []string          `json:"Env"`
	Cmd          []string          `json:"Cmd"`
	Image        string            `json:"Image"`
	Volumes      any               `json:"Volumes"`
	WorkingDir   string            `json:"WorkingDir"`
	Entrypoint   any               `json:"Entrypoint"`
	OnBuild      any               `json:"OnBuild"`
	Labels       map[string]string `json:"Labels"`
}

type ImageConfig struct {
	Config       ImageConfigInfo      `json:"config"`
	Architecture string               `json:"architecture"`
	History      []ImageConfigHistory `json:"history"`
}

func ParseAuthenticate(value string) (map[string]string, error) {
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

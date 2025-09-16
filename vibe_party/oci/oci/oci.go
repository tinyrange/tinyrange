package oci

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/tinyrange/tinyrange/archive"
)

// Minimal types for OCI/Docker manifests and indices.
type tokenResponse struct {
	Token string `json:"token"`
}

type platform struct {
	Architecture string `json:"architecture"`
	OS           string `json:"os"`
}

type descriptor struct {
	MediaType string   `json:"mediaType"`
	Digest    string   `json:"digest"`
	Size      int64    `json:"size"`
	Platform  platform `json:"platform"`
}

type imageIndex struct {
	SchemaVersion int          `json:"schemaVersion"`
	MediaType     string       `json:"mediaType"`
	Manifests     []descriptor `json:"manifests"`
}

type imageManifest struct {
	SchemaVersion int          `json:"schemaVersion"`
	MediaType     string       `json:"mediaType"`
	Config        descriptor   `json:"config"`
	Layers        []descriptor `json:"layers"`
}

// WWW-Authenticate header parser for Bearer challenge
func parseAuthenticate(h string) map[string]string {
	res := map[string]string{}
	if h == "" {
		return res
	}
	// example: Bearer realm="https://auth.docker.io/token",service="registry.docker.io",scope="repository:library/busybox:pull"
	parts := strings.SplitN(h, " ", 2)
	if len(parts) != 2 {
		return res
	}
	for _, kv := range strings.Split(parts[1], ",") {
		kv = strings.TrimSpace(kv)
		if kv == "" {
			continue
		}
		pair := strings.SplitN(kv, "=", 2)
		if len(pair) != 2 {
			continue
		}
		key := strings.TrimSpace(pair[0])
		val := strings.Trim(pair[1], "\"")
		res[key] = val
	}
	return res
}

type Client struct {
	HTTPClient *http.Client
	Registry   string
	Token      string
}

func (c *Client) do(req *http.Request) (*http.Response, error) {
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	return c.HTTPClient.Do(req)
}

func (c *Client) getWithAuth(url string, accept []string) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	for _, a := range accept {
		req.Header.Add("Accept", a)
	}
	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		// attempt token fetch
		auth := parseAuthenticate(resp.Header.Get("Www-Authenticate"))
		resp.Body.Close()
		realm := auth["realm"]
		service := auth["service"]
		scope := auth["scope"]
		if realm == "" || service == "" || scope == "" {
			return nil, fmt.Errorf("unauthorized and missing auth challenge details")
		}
		tokenURL := fmt.Sprintf("%s?service=%s&scope=%s", realm, service, scope)
		tresp, err := c.HTTPClient.Get(tokenURL)
		if err != nil {
			return nil, err
		}
		defer tresp.Body.Close()
		if tresp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("token fetch failed: %s", tresp.Status)
		}
		var t tokenResponse
		if err := json.NewDecoder(tresp.Body).Decode(&t); err != nil {
			return nil, err
		}
		c.Token = t.Token
		// retry
		req2, _ := http.NewRequest("GET", url, nil)
		for _, a := range accept {
			req2.Header.Add("Accept", a)
		}
		return c.do(req2)
	}
	return resp, nil
}

// FetchImage fetches an OCI image and writes a merged archive to ark.
type entryWriter interface {
	WriteEntry(entry *archive.Entry, r io.Reader) error
}

func (c *Client) FetchImage(image string, reference string, architecture string, ark entryWriter) error {
	// Step 1: fetch tag/digest index
	idxURL := fmt.Sprintf("%s/%s/manifests/%s", c.Registry, image, reference)
	resp, err := c.getWithAuth(idxURL, []string{
		"application/vnd.docker.distribution.manifest.list.v2+json",
		"application/vnd.oci.image.index.v1+json",
		"application/vnd.docker.distribution.manifest.v2+json",
		"application/vnd.oci.image.manifest.v1+json",
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("index fetch failed: %s", resp.Status)
	}

	// Read the payload into memory (small JSON)
	var idx imageIndex
	var man imageManifest
	dec := json.NewDecoder(resp.Body)
	// Peek by trying to decode as index first; if that fails, decode manifest
	if err := dec.Decode(&idx); err == nil && len(idx.Manifests) > 0 {
		// choose platform
		sel := descriptor{}
		for _, d := range idx.Manifests {
			if architecture == "" || strings.EqualFold(d.Platform.Architecture, architecture) {
				sel = d
				break
			}
		}
		if sel.Digest == "" {
			return fmt.Errorf("no matching manifest for arch %q", architecture)
		}
		// fetch manifest by digest
		mURL := fmt.Sprintf("%s/%s/manifests/%s", c.Registry, image, sel.Digest)
		mresp, err := c.getWithAuth(mURL, []string{"application/vnd.oci.image.manifest.v1+json", "application/vnd.docker.distribution.manifest.v2+json"})
		if err != nil {
			return err
		}
		if mresp.StatusCode != http.StatusOK {
			mresp.Body.Close()
			return fmt.Errorf("manifest fetch failed: %s", mresp.Status)
		}
		if err := json.NewDecoder(mresp.Body).Decode(&man); err != nil {
			mresp.Body.Close()
			return err
		}
		mresp.Body.Close()
	} else {
		// Re-decode as manifest (need to refetch body; simplest is to refetch URL)
		resp2, err := c.getWithAuth(idxURL, []string{"application/vnd.oci.image.manifest.v1+json", "application/vnd.docker.distribution.manifest.v2+json"})
		if err != nil {
			return err
		}
		if resp2.StatusCode != http.StatusOK {
			resp2.Body.Close()
			return fmt.Errorf("manifest fetch failed: %s", resp2.Status)
		}
		if err := json.NewDecoder(resp2.Body).Decode(&man); err != nil {
			resp2.Body.Close()
			return err
		}
		resp2.Body.Close()
	}

	// Step 2: stream each layer tar into ark
	for _, layer := range man.Layers {
		blobURL := fmt.Sprintf("%s/%s/blobs/%s", c.Registry, image, layer.Digest)
		bresp, err := c.getWithAuth(blobURL, []string{"application/vnd.oci.image.layer.v1.tar+gzip", "application/vnd.docker.image.rootfs.diff.tar.gzip", "application/octet-stream"})
		if err != nil {
			return err
		}
		if bresp.StatusCode != http.StatusOK {
			bresp.Body.Close()
			return fmt.Errorf("layer fetch failed: %s", bresp.Status)
		}

		// Try gzip; if it fails, treat as plain tar
		var rc io.ReadCloser = bresp.Body
		gr, gzErr := gzip.NewReader(bresp.Body)
		if gzErr == nil {
			rc = gr
		}

		tr := tar.NewReader(rc)
		for {
			hdr, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				rc.Close()
				bresp.Body.Close()
				return err
			}

			// Whiteout handling (simplified): skip whiteout marker files
			base := hdr.Name
			if i := strings.LastIndex(base, "/"); i != -1 {
				base = base[i+1:]
			}
			if strings.HasPrefix(base, ".wh.") {
				// TODO: optionally encode deletions via EntryKindDeleted
				continue
			}

			info := hdr.FileInfo()
			var kind archive.EntryKind
			switch hdr.Typeflag {
			case tar.TypeReg:
				kind = archive.EntryKindRegular
			case tar.TypeDir:
				kind = archive.EntryKindDirectory
			case tar.TypeSymlink:
				kind = archive.EntryKindSymlink
			case tar.TypeLink:
				kind = archive.EntryKindHardlink
			default:
				continue
			}

			if err := ark.WriteEntry(&archive.Entry{
				Kind:     kind,
				Name:     hdr.Name,
				Linkname: hdr.Linkname,
				Size:     hdr.Size,
				Mode:     info.Mode(),
				Uid:      hdr.Uid,
				Gid:      hdr.Gid,
				ModTime:  hdr.ModTime,
			}, tr); err != nil {
				rc.Close()
				bresp.Body.Close()
				return err
			}
		}
		if gr != nil {
			rc.Close()
		}
		bresp.Body.Close()
	}
	return nil
}

package core

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"

	"github.com/schollz/progressbar/v3"
)

var (
	ErrNotFound = fmt.Errorf("http not found")
)

type EnvironmentInterface struct {
	cachePath string
	client    *http.Client
}

func (eif *EnvironmentInterface) urlToPath(key string) (string, error) {
	u, err := url.Parse(key)
	if err != nil {
		return "", err
	}

	keyPath := path.Clean(u.Hostname() + u.EscapedPath() + ".cache")

	return filepath.Join(eif.cachePath, keyPath), nil
}

func (eif *EnvironmentInterface) HttpGetReader(url string) (io.ReadCloser, error) {
	path, err := eif.urlToPath(url)
	if err != nil {
		return nil, err
	}

	if _, err := os.Stat(path); err == nil {
		return os.Open(path)
	}

	if err := os.MkdirAll(filepath.Dir(path), os.ModePerm); err != nil {
		return nil, err
	}

	resp, err := eif.client.Get(url)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	} else if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http error: %s", resp.Status)
	}

	out, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	defer out.Close()

	pb := progressbar.DefaultBytes(resp.ContentLength, fmt.Sprintf("downloading %s", url))
	defer pb.Close()

	if _, err := io.Copy(io.MultiWriter(pb, out), resp.Body); err != nil {
		return nil, err
	}

	return eif.HttpGetReader(url)
}

func NewEif(cachePath string) *EnvironmentInterface {
	return &EnvironmentInterface{
		cachePath: cachePath,
		client:    &http.Client{},
	}
}

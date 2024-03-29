package core

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"time"

	"github.com/schollz/progressbar/v3"
)

var CACHE_EXPIRE_RULES = []struct {
	re     *regexp.Regexp
	maxAge time.Duration
}{
	// Alpine Linux
	{regexp.MustCompile(".*/APKINDEX.tar.gz"), 24 * time.Hour},

	// Ubuntu/Debian
	{regexp.MustCompile(".*/archive/dists/.*/InRelease"), 24 * time.Hour},
	{regexp.MustCompile(".*/archive/dists/.*/by-hash/.*"), 24 * time.Hour},

	// Alpine Linux
	{regexp.MustCompile(".*.db.tar.gz"), 24 * time.Hour},
}

var (
	ErrNotFound = fmt.Errorf("http not found")
)

type EnvironmentInterface struct {
	cachePath string
	client    *http.Client
}

func (eif *EnvironmentInterface) needsRefresh(url string, age time.Duration) bool {
	for _, rule := range CACHE_EXPIRE_RULES {
		if rule.re.Match([]byte(url)) {
			return age > rule.maxAge
		}
	}

	return false
}

func (eif *EnvironmentInterface) GetCachePath(key string) (string, error) {
	u, err := url.Parse(key)
	if err != nil {
		return "", err
	}

	keyPath := path.Clean(u.Hostname() + u.EscapedPath() + ".cache")

	return filepath.Join(eif.cachePath, keyPath), nil
}

func (eif *EnvironmentInterface) HttpGetReader(url string) (io.ReadCloser, error) {
	path, err := eif.GetCachePath(url)
	if err != nil {
		return nil, err
	}

	if info, err := os.Stat(path); err == nil {
		// slog.Info("checking refresh", "url", url, "age", time.Since(info.ModTime()))
		if !eif.needsRefresh(url, time.Since(info.ModTime())) {
			return os.Open(path)
		}
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

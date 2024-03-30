package core

import (
	"encoding/gob"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
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

	keyPath := filepath.Clean(u.Hostname() + u.EscapedPath() + ".cache")

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

// A generic interface to caching arbitrary data.
// Right now this saves to the filesystem but that should not be assumed long term.
// The filename it writes to is considered a internal implementation detail.
// This method is not currently thread-safe.
func (eif *EnvironmentInterface) Cache(
	key string,
	version int,
	expire time.Duration,
	miss func(w io.Writer) error,
) (io.Reader, error) {
	filename := filepath.Join(eif.cachePath, fmt.Sprintf("managedCache/%s_%d.bin", key, version))

	if info, err := os.Stat(filename); err == nil {
		if time.Since(info.ModTime()) < expire {
			return os.Open(filename)
		}
	}

	if err := os.MkdirAll(filepath.Dir(filename), os.ModePerm); err != nil {
		return nil, err
	}

	tmpFilename := filename + ".tmp"

	f, err := os.Create(tmpFilename)
	if err != nil {
		return nil, err
	}

	if err := miss(f); err != nil {
		f.Close()
		return nil, err
	}

	if err := f.Close(); err != nil {
		return nil, err
	}

	if err := os.Rename(tmpFilename, filename); err != nil {
		return nil, err
	}

	return os.Open(filename)
}

// A higher level interface for caching arbitrary objects.
func (eif *EnvironmentInterface) CacheObjects(
	key string,
	version int,
	expire time.Duration,
	// Repeatedly call write with a object.
	miss func(write func(obj any) error) error,
	// Repeatedly call read with a object. When EOF is returned discord the object and return nil.
	decode func(read func(obj any) error) error,
) error {
	contents, err := eif.Cache(key, version, expire, func(w io.Writer) error {
		enc := gob.NewEncoder(w)

		if err := miss(func(obj any) error {
			return enc.Encode(obj)
		}); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return err
	}

	dec := gob.NewDecoder(contents)

	if err := decode(func(obj any) error {
		return dec.Decode(obj)
	}); err != nil {
		return err
	}

	return nil
}

func NewEif(cachePath string) *EnvironmentInterface {
	return &EnvironmentInterface{
		cachePath: cachePath,
		client:    &http.Client{},
	}
}

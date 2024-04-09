package core

import (
	"encoding/gob"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/schollz/progressbar/v3"
)

var REFRESH_TIME = 1 * time.Hour

var CACHE_EXPIRE_RULES = []struct {
	re     *regexp.Regexp
	maxAge time.Duration
}{
	// Alpine Linux
	{regexp.MustCompile(".*/APKINDEX.tar.gz"), REFRESH_TIME},

	// Ubuntu/Debian
	{regexp.MustCompile(".*/Packages.gz"), REFRESH_TIME},

	// Arch Linux
	{regexp.MustCompile(".*.db.tar.gz"), REFRESH_TIME},
	{regexp.MustCompile(".*/packages-meta-ext-v1.json.gz"), REFRESH_TIME},

	// RPM
	{regexp.MustCompile(".*/repodata/repomd.xml"), REFRESH_TIME},

	// Conda
	{regexp.MustCompile(".*/repodata.json"), REFRESH_TIME},

	// XBPS
	{regexp.MustCompile(".*/.*-repodata"), REFRESH_TIME},
}

var (
	ErrNotFound = fmt.Errorf("http not found")
)

type EnvironmentInterface struct {
	cachePath string
	client    *http.Client
	httpMutex sync.Mutex
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

type HttpOptions struct {
	ExpectedSize int64
	Accept       string
	UseETag      bool
	FastDownload bool
}

func (eif *EnvironmentInterface) HttpGetReader(url string, options HttpOptions) (io.ReadCloser, error) {
	path, err := eif.GetCachePath(url)
	if err != nil {
		return nil, err
	}

	if info, err := os.Stat(path); err == nil {
		// slog.Info("checking refresh", "url", url, "age", time.Since(info.ModTime()))
		if options.ExpectedSize != 0 && info.Size() != options.ExpectedSize {
			// Assume the file is corrupted and fall though.
		} else {
			if !eif.needsRefresh(url, time.Since(info.ModTime())) {
				return os.Open(path)
			} else {
				slog.Info("checking server for updates", "url", url)
				resp, err := eif.client.Head(url)
				if err != nil {
					return nil, err
				}

				lastModified := resp.Header.Get("Last-Modified")
				if lastModified != "" {
					date, err := time.Parse(http.TimeFormat, lastModified)
					if err == nil && date.After(info.ModTime()) {
						// fall through
					} else {
						if err := os.Chtimes(path, time.Now(), time.Now()); err != nil {
							return nil, err
						}

						return os.Open(path)
					}
				} else {
					if err := os.Chtimes(path, time.Now(), time.Now()); err != nil {
						return nil, err
					}

					return os.Open(path)
				}
			}
		}
	}

	// miss

	if !options.FastDownload {
		// Lock the mutex to prevent concurrent downloads.
		eif.httpMutex.Lock()
		defer eif.httpMutex.Unlock()
	}

	if err := os.MkdirAll(filepath.Dir(path), os.ModePerm); err != nil {
		return nil, err
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	if options.Accept != "" {
		req.Header.Add("Accept", options.Accept)
	}

	resp, err := eif.client.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	} else if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http error: %s", resp.Status)
	}

	if options.ExpectedSize != 0 && resp.ContentLength != options.ExpectedSize {
		return nil, fmt.Errorf("size mismatch: %d != %d", resp.ContentLength, options.ExpectedSize)
	}

	tmpFilename := path + ".tmp"

	out, err := os.Create(tmpFilename)
	if err != nil {
		return nil, err
	}

	var writer io.Writer = out

	if resp.ContentLength < 100000 {
		// Don't display the progress bar for downloads under 100k
	} else {
		pb := progressbar.DefaultBytes(resp.ContentLength, fmt.Sprintf("downloading %s", url))
		defer pb.Close()

		writer = io.MultiWriter(pb, out)
	}

	var n int64
	if n, err = io.Copy(writer, resp.Body); err != nil {
		out.Close()
		return nil, err
	}

	if options.ExpectedSize != 0 && n != options.ExpectedSize {
		out.Close()
		return nil, fmt.Errorf("size mismatch: %d != %d", n, options.ExpectedSize)
	}

	if err := out.Close(); err != nil {
		return nil, err
	}

	if err := os.Rename(tmpFilename, path); err != nil {
		return nil, err
	}

	return eif.HttpGetReader(url, options)
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

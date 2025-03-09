package archive2

import (
	"compress/zlib"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"time"

	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/filesystem/vm"
)

type CVMFSArchiveChunk struct {
	Hash   string
	Offset int64
	Size   int64
}

const (
	CVMFS_ARCHIVE_METADATA_KIND = "cvmfs-archive-metadata/v1"
)

type CVMFSArchiveMetadata struct {
	Kind    string
	Mirrors []string
	Repo    string
	Chunks  []CVMFSArchiveChunk
}

func (m CVMFSArchiveMetadata) UrlFor(hash string, isPartial bool) string {
	suffix := ""
	if isPartial {
		suffix = "P"
	}
	return m.Mirrors[0] + "/" + m.Repo + "/data/" + hash[:2] + "/" + hash[2:] + suffix
}

func (m CVMFSArchiveMetadata) Size() int64 {
	var size int64

	for _, chunk := range m.Chunks {
		size += chunk.Size
	}

	return size
}

type cvmfsChunk struct {
	ctx  filesystem.ExtendedRegionMethods
	hash string
	url  string
	size int64

	handle io.ReaderAt
}

// ReadAt implements vm.MemoryRegion.
func (c *cvmfsChunk) ReadAt(p []byte, off int64) (n int, err error) {
	if c.handle == nil {
		handle, err := c.ctx.GetOrSetCacheForHash(c.hash, func(w io.Writer) error {
			client := c.ctx.HttpClient()

			// Fetch the chunk from the CVMFS server.
			resp, err := client.Get(c.url)
			if err != nil {
				return fmt.Errorf("failed to get: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != 200 {
				return fmt.Errorf("unexpected status: %s", resp.Status)
			}

			// Decompress the chunk with zlib.
			zlibReader, err := zlib.NewReader(resp.Body)
			if err != nil {
				return fmt.Errorf("failed to create zlib reader: %w", err)
			}
			defer zlibReader.Close()

			if _, err := io.Copy(w, zlibReader); err != nil {
				return fmt.Errorf("failed to copy: %w", err)
			}

			return nil
		})
		if err != nil {
			return 0, fmt.Errorf("failed to get or set cache for hash: %w", err)
		}

		c.handle = handle
	}

	n, err = c.handle.ReadAt(p, off)
	if err == io.EOF {
		err = nil
	}

	return n, err
}

// Size implements vm.MemoryRegion.
func (c *cvmfsChunk) Size() int64 {
	return c.size
}

// WriteAt implements vm.MemoryRegion.
func (c *cvmfsChunk) WriteAt(p []byte, off int64) (n int, err error) {
	return 0, fmt.Errorf("WriteAt on cvmfsChunk not implemented")
}

var (
	_ vm.MemoryRegion = &cvmfsChunk{}
)

type cvmfsFile struct {
	metadata CVMFSArchiveMetadata

	ctx    filesystem.ExtendedRegionMethods
	region vm.MemoryRegion

	modTime time.Time
	mode    fs.FileMode
	uid     int
	gid     int
	name    string
}

// ReadAt implements vm.MemoryRegion.
func (c *cvmfsFile) ReadAt(p []byte, off int64) (n int, err error) {
	if c.region == nil {
		var regionArray vm.RegionArray[vm.MemoryRegion]

		for _, chunk := range c.metadata.Chunks {
			regionArray = append(regionArray, &cvmfsChunk{
				ctx:  c.ctx,
				hash: chunk.Hash,
				url:  c.metadata.UrlFor(chunk.Hash, len(c.metadata.Chunks) > 1),
				size: chunk.Size,
			})
		}

		if regionArray.Size() != c.metadata.Size() {
			return 0, fmt.Errorf(
				"regionArray.Size() != c.metadata.Size(): %d != %d",
				regionArray.Size(), c.metadata.Size(),
			)
		}

		c.region = &regionArray
	}

	return c.region.ReadAt(p, off)
}

// WriteAt implements vm.MemoryRegion.
func (c *cvmfsFile) WriteAt(p []byte, off int64) (n int, err error) {
	return 0, fmt.Errorf("WriteAt on cvmfsFile not implemented")
}

// OpenRegion implements filesystem.HasOpenRegion.
func (c *cvmfsFile) OpenRegion(ctx filesystem.ExtendedRegionMethods) (vm.MemoryRegion, error) {
	c.ctx = ctx
	return c, nil
}

// UidAndGid implements filesystem.HasUidAndGid.
func (c *cvmfsFile) UidAndGid() (int, int, error) {
	return c.uid, c.gid, nil
}

// Open implements filesystem.File.
func (c *cvmfsFile) Open() (filesystem.FileHandle, error) {
	return nil, fmt.Errorf("Open on cvmfsFile not implemented")
}

// Stat implements filesystem.File.
func (c *cvmfsFile) Stat() (filesystem.FileInfo, error) {
	return c, nil
}

func (a *cvmfsFile) IsDir() bool               { return a.Mode().IsDir() }
func (a *cvmfsFile) Kind() filesystem.FileType { return filesystem.TypeRegular }
func (a *cvmfsFile) ModTime() time.Time        { return a.modTime }
func (a *cvmfsFile) Mode() fs.FileMode         { return a.mode }
func (a *cvmfsFile) Name() string              { return a.name }
func (a *cvmfsFile) Size() int64               { return a.metadata.Size() }
func (a *cvmfsFile) Sys() any                  { return nil }

var (
	_ filesystem.File          = &cvmfsFile{}
	_ filesystem.HasOpenRegion = &cvmfsFile{}
	_ filesystem.FileInfo      = &cvmfsFile{}
	_ filesystem.HasUidAndGid  = &cvmfsFile{}
	_ vm.MemoryRegion          = &cvmfsFile{}
)

type ExtendedHeader struct {
	Kind string
}

func fileFromExtendedEntry(ar *ArchiveReader) (filesystem.File, error) {
	fh, err := ar.Open()
	if err != nil {
		return nil, err
	}

	contents, err := io.ReadAll(fh)
	if err != nil {
		return nil, err
	}

	var hdr ExtendedHeader

	if err := json.Unmarshal(contents, &hdr); err != nil {
		return nil, err
	}

	switch hdr.Kind {
	case CVMFS_ARCHIVE_METADATA_KIND:
		f := &cvmfsFile{}

		uid, gid := ar.Owner()
		f.uid = uid
		f.gid = gid

		f.modTime = ar.ModTime()
		f.mode = ar.Mode()

		if err := json.Unmarshal(contents, &f.metadata); err != nil {
			return nil, err
		}

		return f, nil
	default:
		return nil, fmt.Errorf("unknown extended entry kind: %s", hdr.Kind)
	}
}

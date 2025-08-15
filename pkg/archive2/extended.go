package archive2

import (
	"compress/zlib"
	"encoding/json"
	"fmt"
	"hash/adler32"
	"io"
	"io/fs"
	"time"

	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/filesystem/vm"
	"github.com/tinyrange/tinyrange/pkg/log"
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
	ctx  filesystem.ExtendedFileMethods
	hash string
	url  string
	size int64

	handle io.ReaderAt
}

// ReadAt implements vm.MemoryRegion.
func (c *cvmfsChunk) ReadAt(p []byte, off int64) (n int, err error) {
	if c.handle == nil {
		handle, err := c.ctx.GetOrSetCacheForHash(c.hash, func(w io.Writer) error {
			client, err := c.ctx.HttpClient()
			if err != nil {
				return fmt.Errorf("failed to get http client: %w", err)
			}

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

			var writer = w
			if common.IsVerbose() {
				pb := log.Default().NewProgressBarBytes(c.size, c.hash)
				defer pb.Close()

				writer = io.MultiWriter(w, pb)
			}

			if _, err := io.Copy(writer, zlibReader); err != nil {
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

type simpleCvmfsHandle struct {
	vm.MemoryRegion
	io.Reader
}

// Close implements filesystem.FileHandle.
func (s *simpleCvmfsHandle) Close() error {
	return nil
}

var (
	_ filesystem.FileHandle = &simpleCvmfsHandle{}
)

type cvmfsFile struct {
	metadata CVMFSArchiveMetadata

	ctx    filesystem.ExtendedFileMethods
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
			regionArray.Append(&cvmfsChunk{
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
func (c *cvmfsFile) OpenRegion() (vm.MemoryRegion, error) {
	if c.ctx == nil {
		return nil, fmt.Errorf("cvmfsFile has no context")
	}

	return c, nil
}

// UidAndGid implements filesystem.HasUidAndGid.
func (c *cvmfsFile) UidAndGid() (int, int, error) {
	return c.uid, c.gid, nil
}

// Open implements filesystem.File.
func (c *cvmfsFile) Open() (filesystem.FileHandle, error) {
	region, err := c.OpenRegion()
	if err != nil {
		return nil, fmt.Errorf("failed to open region: %w", err)
	}

	return &simpleCvmfsHandle{
		MemoryRegion: region,
		Reader:       io.NewSectionReader(region, 0, region.Size()),
	}, nil
}

// Stat implements filesystem.File.
func (c *cvmfsFile) Stat() (filesystem.FileInfo, error) {
	return c, nil
}

func (c *cvmfsFile) Id() uint64                { return uint64(adler32.Checksum([]byte(c.metadata.Chunks[0].Hash))) }
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

func fileFromExtendedEntry(ar *ArchiveReader, methods filesystem.ExtendedFileMethods) (filesystem.File, error) {
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
		f := &cvmfsFile{
			ctx: methods,
		}

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

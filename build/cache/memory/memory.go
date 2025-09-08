package memory

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"io/fs"

	"github.com/tinyrange/tinyrange/build/internal/common"
	"github.com/tinyrange/tinyrange/build/proto"
)

type memoryFile struct {
	*bytes.Reader
}

func (m *memoryFile) Close() error {
	return nil
}

type writableMemoryFile struct {
	buf  bytes.Buffer
	hash hash.Hash
}

func (w *writableMemoryFile) Hash() *proto.Hash {
	sum := w.hash.Sum(nil)
	return &proto.Hash{
		Value: hex.EncodeToString(sum),
	}
}

func (w *writableMemoryFile) Write(p []byte) (n int, err error) {
	n, err = w.buf.Write(p)
	if err != nil {
		return n, err
	}
	if _, err := w.hash.Write(p); err != nil {
		return n, err
	}
	return n, nil
}

func (w *writableMemoryFile) Close() error {
	return nil
}

type memoryCacheEntry struct {
	definition []byte
	receipt    []byte
	files      map[common.FileType]*writableMemoryFile
}

// CreateFile implements common.WritableBuildCacheDirectory.
func (m *memoryCacheEntry) CreateFile(ft common.FileType) (common.WritableFile, error) {
	w := &writableMemoryFile{
		hash: sha256.New(),
	}
	m.files[ft] = w
	return w, nil
}

// WriteDefinition implements common.WritableBuildCacheDirectory.
func (m *memoryCacheEntry) WriteDefinition(content []byte) error {
	m.definition = make([]byte, len(content))
	copy(m.definition, content)
	return nil
}

// WriteReceipt implements common.WritableBuildCacheDirectory.
func (m *memoryCacheEntry) WriteReceipt(content []byte) error {
	m.receipt = make([]byte, len(content))
	copy(m.receipt, content)
	return nil
}

// OpenFile implements common.BuildCacheDirectory.
func (m *memoryCacheEntry) OpenFile(ft common.FileType) (common.File, error) {
	data, ok := m.files[ft]
	if !ok {
		return nil, fs.ErrNotExist
	}

	return &memoryFile{
		Reader: bytes.NewReader(data.buf.Bytes()),
	}, nil
}

// ReadDefinition implements common.BuildCacheDirectory.
func (m *memoryCacheEntry) ReadDefinition() ([]byte, error) {
	out := make([]byte, len(m.definition))
	copy(out, m.definition)
	return out, nil
}

// ReadReceipt implements common.BuildCacheDirectory.
func (m *memoryCacheEntry) ReadReceipt() ([]byte, error) {
	out := make([]byte, len(m.receipt))
	copy(out, m.receipt)
	return out, nil
}

type memoryCache struct {
	entries map[string]*memoryCacheEntry
}

// OpenRead implements common.BuildCache.
func (m *memoryCache) OpenRead(h *proto.Hash) (common.BuildCacheDirectory, error) {
	entry, ok := m.entries[h.String()]
	if !ok {
		return nil, fs.ErrNotExist
	}

	return entry, nil
}

// OpenWrite implements common.BuildCache.
func (m *memoryCache) OpenWrite(h *proto.Hash) (common.WritableBuildCacheDirectory, error) {
	entry := &memoryCacheEntry{
		files: make(map[common.FileType]*writableMemoryFile),
	}
	m.entries[h.String()] = entry
	return entry, nil
}

var (
	_ common.BuildCache                  = (*memoryCache)(nil)
	_ common.BuildCacheDirectory         = (*memoryCacheEntry)(nil)
	_ common.WritableBuildCacheDirectory = (*memoryCacheEntry)(nil)
)

func NewCache() common.BuildCache {
	return &memoryCache{
		entries: make(map[string]*memoryCacheEntry),
	}
}

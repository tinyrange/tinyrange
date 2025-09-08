package memstore

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"

	"github.com/tinyrange/tinyrange/vibe_party/minigit/internal/storage"
)

type memObjects struct {
	mu   sync.RWMutex
	objs map[storage.Hash]*storage.Object
}

func newMemObjects() *memObjects {
	return &memObjects{objs: make(map[storage.Hash]*storage.Object)}
}

func (m *memObjects) Has(h storage.Hash) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.objs[h]
	return ok, nil
}

func (m *memObjects) Get(h storage.Hash) (*storage.Object, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	obj, ok := m.objs[h]
	if !ok {
		return nil, fmt.Errorf("object not found: %s", h)
	}
	// Return a shallow copy to avoid external mutation
	dup := *obj
	dup.Data = append([]byte(nil), obj.Data...)
	return &dup, nil
}

func headerFor(t storage.ObjectType, size int64) string {
	return (&storage.Object{Type: t, Size: size}).Header()
}

func (m *memObjects) Put(obj *storage.Object) (storage.Hash, error) {
	// Git object id = sha1(header + data)
	hdr := []byte(headerFor(obj.Type, obj.Size))
	sum := sha1.Sum(append(hdr, obj.Data...))
	h := storage.Hash(hex.EncodeToString(sum[:]))
	m.mu.Lock()
	m.objs[h] = &storage.Object{Type: obj.Type, Size: obj.Size, Data: append([]byte(nil), obj.Data...)}
	m.mu.Unlock()
	return h, nil
}

func (m *memObjects) Iter(fn func(h storage.Hash, t storage.ObjectType, size int64) error) error {
	m.mu.RLock()
	keys := make([]string, 0, len(m.objs))
	for k := range m.objs {
		keys = append(keys, string(k))
	}
	m.mu.RUnlock()
	sort.Strings(keys)
	for _, k := range keys {
		h := storage.Hash(k)
		m.mu.RLock()
		obj := m.objs[h]
		m.mu.RUnlock()
		if err := fn(h, obj.Type, obj.Size); err != nil {
			return err
		}
	}
	return nil
}

type memRefs struct {
	mu       sync.RWMutex
	refs     map[string]storage.Hash
	defaults string
}

func newMemRefs() *memRefs {
	return &memRefs{refs: make(map[string]storage.Hash), defaults: "refs/heads/main"}
}

func (m *memRefs) List() (map[string]storage.Hash, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]storage.Hash, len(m.refs))
	for k, v := range m.refs {
		out[k] = v
	}
	return out, nil
}

func (m *memRefs) Get(name string) (storage.Hash, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.refs[name]
	return v, ok, nil
}

func (m *memRefs) Set(name string, h storage.Hash) error {
	m.mu.Lock()
	m.refs[name] = h
	m.mu.Unlock()
	return nil
}

func (m *memRefs) CompareAndSwap(name string, old storage.Hash, new storage.Hash) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.refs[name]
	if !ok {
		if old != "" {
			return false, nil
		}
		m.refs[name] = new
		return true, nil
	}
	if old != "" && cur != old {
		return false, nil
	}
	m.refs[name] = new
	return true, nil
}

func (m *memRefs) DefaultBranch() (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.defaults, nil
}

func (m *memRefs) SetDefaultBranch(name string) error {
	if !strings.HasPrefix(name, "refs/") {
		name = "refs/heads/" + name
	}
	m.mu.Lock()
	m.defaults = name
	m.mu.Unlock()
	return nil
}

type repo struct {
	objects *memObjects
	refs    *memRefs
}

func (r *repo) Objects() storage.ObjectStore { return r.objects }
func (r *repo) Refs() storage.RefStore       { return r.refs }

// Registry is an in-memory collection of bare repos keyed by name.
type Registry struct {
	mu    sync.RWMutex
	repos map[string]*repo
}

func NewRegistry() *Registry { return &Registry{repos: make(map[string]*repo)} }

func (r *Registry) Get(name string) (storage.Repo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.repos[name]
	return v, ok
}

func (r *Registry) CreateBare(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.repos[name]; exists {
		return fmt.Errorf("repo exists: %s", name)
	}
	r.repos[name] = &repo{objects: newMemObjects(), refs: newMemRefs()}
	return nil
}

// ImportPack reads a raw git pack stream and applies it into the repo named.
// This is a placeholder that currently buffers the stream; the actual decoding
// is delegated to the HTTP receive-pack handler using go-git.
func (r *Registry) ImportPack(name string, pr io.Reader, updateFn func(storage.RefStore) error) error {
	// For an in-memory registry without durable import path, we just drain.
	_, _ = io.Copy(io.Discard, pr)
	r.mu.RLock()
	rep := r.repos[name]
	r.mu.RUnlock()
	if rep == nil {
		return fmt.Errorf("repo not found: %s", name)
	}
	if updateFn != nil {
		return updateFn(rep.refs)
	}
	return nil
}

// Convenience helpers for tests/examples
func (r *Registry) PutBlob(repoName string, data []byte) (storage.Hash, error) {
	r.mu.RLock()
	rep := r.repos[repoName]
	r.mu.RUnlock()
	if rep == nil {
		return "", fmt.Errorf("repo not found: %s", repoName)
	}
	return rep.objects.Put(&storage.Object{Type: storage.ObjBlob, Size: int64(len(data)), Data: append([]byte(nil), data...)})
}

func (r *Registry) PutRawObject(repoName string, t storage.ObjectType, data []byte) (storage.Hash, error) {
	r.mu.RLock()
	rep := r.repos[repoName]
	r.mu.RUnlock()
	if rep == nil {
		return "", fmt.Errorf("repo not found: %s", repoName)
	}
	return rep.objects.Put(&storage.Object{Type: t, Size: int64(len(data)), Data: bytes.Clone(data)})
}

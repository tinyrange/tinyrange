package minigit

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/tinyrange/tinyrange/vibe_party/minigit/internal/server"
	istorage "github.com/tinyrange/tinyrange/vibe_party/minigit/internal/storage"
	"github.com/tinyrange/tinyrange/vibe_party/minigit/internal/storage/memstore"
)

// Public, minimal API to use the in-memory Git implementation from other packages.

// Hash is a hex-encoded object ID (SHA-1).
type Hash string

// ObjectType is the Git object type.
type ObjectType int

const (
	ObjInvalid ObjectType = iota
	ObjCommit
	ObjTree
	ObjBlob
	ObjTag
)

// Object represents a raw Git object.
type Object struct {
	Type ObjectType
	Size int64
	Data []byte
}

// ObjectStore provides object access.
type ObjectStore interface {
	Has(h Hash) (bool, error)
	Get(h Hash) (*Object, error)
	Put(obj *Object) (Hash, error)
	Iter(func(h Hash, t ObjectType, size int64) error) error
}

// RefStore manages named refs.
type RefStore interface {
	List() (map[string]Hash, error)
	Get(name string) (Hash, bool, error)
	Set(name string, h Hash) error
	CompareAndSwap(name string, old Hash, new Hash) (bool, error)
	DefaultBranch() (string, error)
	SetDefaultBranch(name string) error
}

// Repo is a bare repository.
type Repo interface {
	Objects() ObjectStore
	Refs() RefStore
}

// Registry stores repos by name.
type Registry interface {
	Get(name string) (Repo, bool)
	CreateBare(name string) error
}

// NewInMemoryRegistry creates a new in-memory registry implementation.
func NewInMemoryRegistry() Registry {
	return &memRegistry{inner: memstore.NewRegistry()}
}

// NewHTTPHandler creates an http.Handler that serves the Git smart protocol
// using the provided registry.
func NewHTTPHandler(reg Registry) (http.Handler, error) {
	// Bridge to the internal server, which expects the internal storage registry.
	if p, ok := reg.(interface{ internal() *memstore.Registry }); ok {
		return server.NewHTTP(p.internal())
	}
	return nil, fmt.Errorf("unsupported registry implementation")
}

// Convenience helpers

// PutObject stores a raw object into a repo and returns its hash.
func PutObject(repo Repo, obj *Object) (Hash, error) {
	return repo.Objects().Put(obj)
}

// GetObject retrieves a raw object by hash.
func GetObject(repo Repo, h Hash) (*Object, error) {
	return repo.Objects().Get(h)
}

// PutBlob stores a blob object from data and returns its hash.
func PutBlob(repo Repo, data []byte) (Hash, error) {
	return repo.Objects().Put(&Object{Type: ObjBlob, Size: int64(len(data)), Data: append([]byte(nil), data...)})
}

// Worktree represents an in-memory staging area for creating commits.
type Worktree interface {
	Add(path string, data []byte)
	Remove(path string)
	WriteFile(path string, data []byte)
	ReadFile(path string) ([]byte, bool, error)
	Readlink(path string) (string, bool, error)
	ListFiles() ([]string, error)
	// SetAuthor sets the author identity for the next commit.
	// If not set, defaults to "minigit <minigit@example>".
	SetAuthor(name, email string)
	// SetCommitter sets the committer identity for the next commit.
	// If not set, it falls back to the author identity.
	SetCommitter(name, email string)
	// SetAuthorTime sets the author timestamp for the next commit.
	// The timezone offset is taken from t's location.
	SetAuthorTime(t time.Time)
	// SetCommitterTime sets the committer timestamp for the next commit.
	// The timezone offset is taken from t's location.
	SetCommitterTime(t time.Time)
	// AddWithMode adds/updates a file with an explicit mode based on fs.FileMode.
	AddWithMode(path string, data []byte, mode fs.FileMode)
	// Mkdir stages a directory entry with mode fs.ModeDir and permissions.
	Mkdir(path string, mode fs.FileMode)
	// AddSymlink adds a symlink whose blob data is the target path; mode 120000.
	AddSymlink(path string, target string)
	// Commit writes tree/blob/commit objects and updates the given branch.
	// If branch is not fully qualified, it assumes refs/heads/<branch>.
	Commit(message string, branch string) (Hash, error)
}

// NewWorktree returns a basic in-memory worktree backed by the repo's object store.
func NewWorktree(repo Repo) Worktree {
	return &wt{repo: repo, files: map[string][]byte{}, modes: map[string]string{}, kinds: map[string]string{}}
}

// NewWorktreeFromCommit initializes the worktree using the tree from baseCommit.
func NewWorktreeFromCommit(repo Repo, baseCommit Hash) (Worktree, error) {
	w := &wt{repo: repo, files: map[string][]byte{}, modes: map[string]string{}, kinds: map[string]string{}, baseCommit: baseCommit}
	if baseCommit != "" {
		obj, err := repo.Objects().Get(baseCommit)
		if err != nil {
			return nil, err
		}
		tree, err := parseCommitTreeLine(obj.Data)
		if err != nil {
			return nil, err
		}
		m, err := readTreeFiles(repo, tree)
		if err != nil {
			return nil, err
		}
		w.baseFiles = m
	}
	return w, nil
}

type wt struct {
	repo             Repo
	files            map[string][]byte // nil value indicates deletion
	baseFiles        map[string]Hash   // optional base snapshot (path->blob)
	baseCommit       Hash              // optional base commit used when no branch parent
	authorName       string
	authorEmail      string
	committerName    string
	committerEmail   string
	authorTime       time.Time
	committerTime    time.Time
	authorTimeSet    bool
	committerTimeSet bool
	modes            map[string]string // path -> git mode string for files/symlinks
	kinds            map[string]string // path -> kind: file|symlink|dir
}

func (w *wt) Add(path string, data []byte) { w.AddWithMode(path, data, 0o644) }
func (w *wt) AddWithMode(path string, data []byte, mode fs.FileMode) {
	w.files[path] = append([]byte(nil), data...)
	w.modes[path] = gitModeFromFileMode(mode)
	w.kinds[path] = kindFromFileMode(mode)
}
func (w *wt) WriteFile(path string, data []byte) {
	m := w.modes[path]
	if m == "" {
		m = gitModeFromFileMode(0o644)
	}
	// convert back to fs perms for API consistency
	w.AddWithMode(path, data, fileModeFromGitMode(m))
}
func (w *wt) Remove(path string) { w.files[path] = nil; w.modes[path] = ""; w.kinds[path] = "" }
func (w *wt) Mkdir(path string, mode fs.FileMode) {
	if mode == 0 {
		mode = fs.ModeDir | 0o755
	}
	w.kinds[path] = "dir"
	w.modes[path] = gitModeFromFileMode(mode)
}
func (w *wt) AddSymlink(path string, target string) {
	w.AddWithMode(path, []byte(target), fs.ModeSymlink)
}

func (w *wt) ReadFile(path string) ([]byte, bool, error) {
	if data, ok := w.files[path]; ok {
		if data == nil {
			return nil, false, nil
		}
		return append([]byte(nil), data...), true, nil
	}
	if w.baseFiles != nil {
		if h, ok := w.baseFiles[path]; ok {
			obj, err := w.repo.Objects().Get(h)
			if err != nil {
				return nil, false, err
			}
			if obj.Type != ObjBlob {
				return nil, false, errors.New("not a blob")
			}
			return append([]byte(nil), obj.Data...), true, nil
		}
	}
	return nil, false, nil
}

func (w *wt) Readlink(path string) (string, bool, error) {
	if w.kinds[path] == "symlink" {
		if data, ok := w.files[path]; ok && data != nil {
			return string(data), true, nil
		}
	}
	if w.baseFiles != nil {
		if h, ok := w.baseFiles[path]; ok {
			obj, err := w.repo.Objects().Get(h)
			if err != nil {
				return "", false, err
			}
			return string(obj.Data), true, nil
		}
	}
	return "", false, nil
}

func (w *wt) ListFiles() ([]string, error) {
	seen := make(map[string]struct{})
	if w.baseFiles != nil {
		for k := range w.baseFiles {
			seen[k] = struct{}{}
		}
	}
	for k, v := range w.files {
		if v == nil {
			delete(seen, k)
			continue
		}
		seen[k] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sortStrings(out)
	return out, nil
}

func (w *wt) SetAuthor(name, email string) { w.authorName, w.authorEmail = name, email }

func (w *wt) SetCommitter(name, email string) { w.committerName, w.committerEmail = name, email }

func (w *wt) SetAuthorTime(t time.Time) { w.authorTime, w.authorTimeSet = t, true }

func (w *wt) SetCommitterTime(t time.Time) { w.committerTime, w.committerTimeSet = t, true }

func (w *wt) Commit(message string, branch string) (Hash, error) {
	if branch == "" {
		branch = "refs/heads/main"
	}
	if !strings.HasPrefix(branch, "refs/") {
		branch = "refs/heads/" + branch
	}

	// Load base entries (with mode) from base or HEAD
	type treeEnt struct {
		mode, kind string
		hash       Hash
	}
	base := make(map[string]treeEnt)
	if w.baseFiles != nil {
		for k, v := range w.baseFiles {
			base[k] = treeEnt{mode: "100644", kind: "file", hash: v}
		}
	} else if head, ok, _ := w.repo.Refs().Get(branch); ok && head != "" {
		c, err := w.repo.Objects().Get(head)
		if err != nil {
			return "", err
		}
		if treeH, err := parseCommitTreeLine(c.Data); err == nil && treeH != "" {
			em, err := readTreeEntries(w.repo, treeH)
			if err != nil {
				return "", err
			}
			for k, v := range em {
				if v.kind != "dir" {
					base[k] = v
				}
			}
		}
	}

	// Overlay staged changes; nil means delete
	final := make(map[string]treeEnt)
	for k, v := range base {
		final[k] = v
	}
	for p, data := range w.files {
		if data == nil {
			delete(final, p)
			continue
		}
		h, err := w.repo.Objects().Put(&Object{Type: ObjBlob, Size: int64(len(data)), Data: data})
		if err != nil {
			return "", err
		}
		mode := w.modes[p]
		if mode == "" {
			mode = gitModeFromFileMode(0o644)
		}
		final[p] = treeEnt{mode: mode, kind: kindFromMode(mode), hash: h}
	}

	// 1) Build tree hierarchy from final entries
	type leaf struct {
		h    Hash
		mode string
	}
	type node struct {
		files map[string]leaf
		dirs  map[string]*node
	}
	root := &node{files: make(map[string]leaf), dirs: make(map[string]*node)}
	for p, e := range final {
		cur := root
		segs := splitPath(p)
		for i := 0; i < len(segs)-1; i++ {
			s := segs[i]
			if cur.dirs[s] == nil {
				cur.dirs[s] = &node{files: make(map[string]leaf), dirs: make(map[string]*node)}
			}
			cur = cur.dirs[s]
		}
		cur.files[segs[len(segs)-1]] = leaf{h: e.hash, mode: e.mode}
	}

	// 2) Recursively write trees
	var writeTree func(n *node) (Hash, error)
	writeTree = func(n *node) (Hash, error) {
		// entries: dirs then files sorted by name
		names := make([]string, 0, len(n.dirs))
		for k := range n.dirs {
			names = append(names, k)
		}
		sortStrings(names)
		var buf []byte
		for _, name := range names {
			th, err := writeTree(n.dirs[name])
			if err != nil {
				return "", err
			}
			raw, _ := hexTo20(string(th))
			// mode 040000 for tree
			buf = append(buf, []byte("040000 ")...)
			buf = append(buf, []byte(name)...)
			buf = append(buf, 0)
			buf = append(buf, raw[:]...)
		}
		fnames := make([]string, 0, len(n.files))
		for k := range n.files {
			fnames = append(fnames, k)
		}
		sortStrings(fnames)
		for _, name := range fnames {
			lf := n.files[name]
			raw, _ := hexTo20(string(lf.h))
			// use leaf mode (file or symlink)
			mode := lf.mode
			if mode == "" {
				mode = gitModeFromFileMode(0o644)
			}
			buf = append(buf, []byte(mode+" ")...)
			buf = append(buf, []byte(name)...)
			buf = append(buf, 0)
			buf = append(buf, raw[:]...)
		}
		return w.repo.Objects().Put(&Object{Type: ObjTree, Size: int64(len(buf)), Data: buf})
	}
	treeHash, err := writeTree(root)
	if err != nil {
		return "", err
	}

	// 3) Create commit object
	refs := w.repo.Refs()
	var parent Hash
	if h, ok, _ := refs.Get(branch); ok && h != "" {
		parent = h
	} else if w.baseCommit != "" {
		parent = w.baseCommit
	}
	aWhen := time.Now()
	if w.authorTimeSet {
		aWhen = w.authorTime
	}
	cWhen := aWhen
	if w.committerTimeSet {
		cWhen = w.committerTime
	}
	authorIdent := "minigit <minigit@example>"
	if w.authorName != "" && w.authorEmail != "" {
		authorIdent = w.authorName + " <" + w.authorEmail + ">"
	}
	committerIdent := authorIdent
	if w.committerName != "" && w.committerEmail != "" {
		committerIdent = w.committerName + " <" + w.committerEmail + ">"
	}
	author := fmt.Sprintf("%s %d %s", authorIdent, aWhen.Unix(), aWhen.Format("-0700"))
	var commitBuf []byte
	commitBuf = append(commitBuf, []byte("tree ")...)
	commitBuf = append(commitBuf, []byte(string(treeHash))...)
	commitBuf = append(commitBuf, '\n')
	if parent != "" {
		commitBuf = append(commitBuf, []byte("parent ")...)
		commitBuf = append(commitBuf, []byte(string(parent))...)
		commitBuf = append(commitBuf, '\n')
	}
	commitBuf = append(commitBuf, []byte("author ")...)
	commitBuf = append(commitBuf, []byte(author)...)
	commitBuf = append(commitBuf, '\n')
	committer := fmt.Sprintf("%s %d %s", committerIdent, cWhen.Unix(), cWhen.Format("-0700"))
	commitBuf = append(commitBuf, []byte("committer ")...)
	commitBuf = append(commitBuf, []byte(committer)...)
	commitBuf = append(commitBuf, '\n', '\n')
	commitBuf = append(commitBuf, []byte(message)...)
	commitBuf = append(commitBuf, '\n')

	commitHash, err := w.repo.Objects().Put(&Object{Type: ObjCommit, Size: int64(len(commitBuf)), Data: commitBuf})
	if err != nil {
		return "", err
	}
	_ = refs.Set(branch, commitHash)
	if def, _ := refs.DefaultBranch(); def == "" || def == "refs/heads/main" {
		_ = refs.SetDefaultBranch(branch)
	}
	return commitHash, nil
}

// splitPath splits a posix-style path into components, trimming empties.
func splitPath(p string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(p); i++ {
		if p[i] == '/' {
			if i > start {
				parts = append(parts, p[start:i])
			}
			start = i + 1
		}
	}
	if start < len(p) {
		parts = append(parts, p[start:])
	}
	if parts == nil {
		return []string{p}
	}
	return parts
}

// hexTo20 decodes a 40-hex string into a 20-byte array.
func hexTo20(s string) ([20]byte, error) {
	var out [20]byte
	for i := 0; i < 20; i++ {
		b := byteFromHex(s[i*2])<<4 | byteFromHex(s[i*2+1])
		out[i] = b
	}
	return out, nil
}

func byteFromHex(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10
	default:
		return 0
	}
}

func sortStrings(a []string) {
	// simple insertion sort to avoid importing sort for small slices
	for i := 1; i < len(a); i++ {
		j := i
		for j > 0 && a[j-1] > a[j] {
			a[j-1], a[j] = a[j], a[j-1]
			j--
		}
	}
}

func kindFromMode(mode string) string {
	if mode == "120000" {
		return "symlink"
	}
	if strings.HasPrefix(mode, "04") {
		return "dir"
	}
	return "file"
}

func kindFromFileMode(m fs.FileMode) string {
	if m&fs.ModeSymlink != 0 {
		return "symlink"
	}
	if m&fs.ModeDir != 0 {
		return "dir"
	}
	return "file"
}

func gitModeFromFileMode(m fs.FileMode) string {
	if m&fs.ModeSymlink != 0 {
		return "120000"
	}
	if m&fs.ModeDir != 0 {
		return "040000"
	}
	if m&0o111 != 0 {
		return "100755"
	}
	return "100644"
}

func fileModeFromGitMode(s string) fs.FileMode {
	switch s {
	case "120000":
		return fs.ModeSymlink
	case "040000":
		return fs.ModeDir | 0o755
	case "100755":
		return 0o755
	default:
		return 0o644
	}
}

// TreeEntry describes a single entry in a tree, for public APIs.
type TreeEntry struct {
	Path string
	Mode fs.FileMode
	Kind string // file|dir|symlink
	Hash Hash
}

// ListTreeEntries lists the entries of a tree recursively.
func ListTreeEntries(repo Repo, tree Hash) ([]TreeEntry, error) {
	raw, err := readTreeEntries(repo, tree)
	if err != nil {
		return nil, err
	}
	out := make([]TreeEntry, 0, len(raw))
	for p, e := range raw {
		out = append(out, TreeEntry{Path: p, Mode: fileModeFromGitMode(e.mode), Kind: e.kind, Hash: e.hash})
	}
	// Sort by path for stable order
	// simple insertion sort
	for i := 1; i < len(out); i++ {
		j := i
		for j > 0 && out[j-1].Path > out[j].Path {
			out[j-1], out[j] = out[j], out[j-1]
			j--
		}
	}
	return out, nil
}

// ListCommitTreeEntries lists entries from a commit's root tree.
func ListCommitTreeEntries(repo Repo, commit Hash) ([]TreeEntry, error) {
	obj, err := repo.Objects().Get(commit)
	if err != nil {
		return nil, err
	}
	tree, err := parseCommitTreeLine(obj.Data)
	if err != nil {
		return nil, err
	}
	return ListTreeEntries(repo, tree)
}

// parseCommitTreeLine extracts the tree hash from a raw commit object.
func parseCommitTreeLine(commit []byte) (Hash, error) {
	// Expect first non-empty line like: "tree <hex>"
	i := 0
	// read until newline
	for i < len(commit) {
		// find end of line
		j := i
		for j < len(commit) && commit[j] != '\n' {
			j++
		}
		line := string(commit[i:j])
		if strings.HasPrefix(line, "tree ") {
			hex := strings.TrimSpace(strings.TrimPrefix(line, "tree "))
			if len(hex) == 40 {
				return Hash(hex), nil
			}
			return "", errors.New("bad tree line")
		}
		if line == "" {
			break
		}
		i = j + 1
	}
	return "", errors.New("no tree line")
}

// readTreeFiles returns a map path->blob hash for the full tree.
func readTreeFiles(repo Repo, tree Hash) (map[string]Hash, error) {
	out := make(map[string]Hash)
	entries, err := readTreeEntries(repo, tree)
	if err != nil {
		return nil, err
	}
	for p, e := range entries {
		if e.kind != "dir" {
			out[p] = e.hash
		}
	}
	return out, nil
}

// readTreeEntries returns a map path->(mode,kind,hash) for the full tree.
func readTreeEntries(repo Repo, tree Hash) (map[string]struct {
	mode, kind string
	hash       Hash
}, error) {
	out := make(map[string]struct {
		mode, kind string
		hash       Hash
	})
	var walk func(Hash, string) error
	walk = func(h Hash, prefix string) error {
		obj, err := repo.Objects().Get(h)
		if err != nil {
			return err
		}
		if obj.Type != ObjTree {
			return errors.New("object not a tree")
		}
		data := obj.Data
		i := 0
		for i < len(data) {
			// parse mode until space
			k := i
			for k < len(data) && data[k] != ' ' {
				k++
			}
			if k >= len(data) {
				break
			}
			mode := string(data[i:k])
			// name until NUL
			k++
			j := k
			for j < len(data) && data[j] != 0 {
				j++
			}
			if j >= len(data) {
				break
			}
			name := string(data[k:j])
			// 20-byte id follows
			j++
			if j+20 > len(data) {
				break
			}
			var raw [20]byte
			copy(raw[:], data[j:j+20])
			child := raw20ToHash(raw)
			if strings.HasPrefix(mode, "04") { // dir
				out[prefix+name] = struct {
					mode, kind string
					hash       Hash
				}{mode: "040000", kind: "dir", hash: child}
				if err := walk(child, prefix+name+"/"); err != nil {
					return err
				}
			} else {
				kind := "file"
				if mode == "120000" {
					kind = "symlink"
				}
				out[prefix+name] = struct {
					mode, kind string
					hash       Hash
				}{mode: mode, kind: kind, hash: child}
			}
			i = j + 20
		}
		return nil
	}
	if err := walk(tree, ""); err != nil {
		return nil, err
	}
	return out, nil
}

func raw20ToHash(raw [20]byte) Hash {
	const hexdigits = "0123456789abcdef"
	buf := make([]byte, 40)
	for i := 0; i < 20; i++ {
		b := raw[i]
		buf[i*2] = hexdigits[b>>4]
		buf[i*2+1] = hexdigits[b&0x0f]
	}
	return Hash(string(buf))
}

// ---- internal adapters ----

type memRegistry struct{ inner *memstore.Registry }

func (m *memRegistry) internal() *memstore.Registry { return m.inner }

func (m *memRegistry) Get(name string) (Repo, bool) {
	r, ok := m.inner.Get(name)
	if !ok {
		return nil, false
	}
	return &repoWrap{inner: r}, true
}

func (m *memRegistry) CreateBare(name string) error { return m.inner.CreateBare(name) }

type repoWrap struct{ inner istorage.Repo }

func (r *repoWrap) Objects() ObjectStore { return &objStoreWrap{inner: r.inner.Objects()} }
func (r *repoWrap) Refs() RefStore       { return &refStoreWrap{inner: r.inner.Refs()} }

type objStoreWrap struct{ inner istorage.ObjectStore }

func (o *objStoreWrap) Has(h Hash) (bool, error) { return o.inner.Has(istorage.Hash(h)) }

func (o *objStoreWrap) Get(h Hash) (*Object, error) {
	obj, err := o.inner.Get(istorage.Hash(h))
	if err != nil {
		return nil, err
	}
	return &Object{Type: ObjectType(obj.Type), Size: obj.Size, Data: append([]byte(nil), obj.Data...)}, nil
}

func (o *objStoreWrap) Put(obj *Object) (Hash, error) {
	h, err := o.inner.Put(&istorage.Object{Type: istorage.ObjectType(obj.Type), Size: obj.Size, Data: append([]byte(nil), obj.Data...)})
	return Hash(h), err
}

func (o *objStoreWrap) Iter(fn func(h Hash, t ObjectType, size int64) error) error {
	return o.inner.Iter(func(h istorage.Hash, t istorage.ObjectType, size int64) error {
		return fn(Hash(h), ObjectType(t), size)
	})
}

type refStoreWrap struct{ inner istorage.RefStore }

func (r *refStoreWrap) List() (map[string]Hash, error) {
	m, err := r.inner.List()
	if err != nil {
		return nil, err
	}
	out := make(map[string]Hash, len(m))
	for k, v := range m {
		out[k] = Hash(v)
	}
	return out, nil
}

func (r *refStoreWrap) Get(name string) (Hash, bool, error) {
	h, ok, err := r.inner.Get(name)
	return Hash(h), ok, err
}

func (r *refStoreWrap) Set(name string, h Hash) error { return r.inner.Set(name, istorage.Hash(h)) }

func (r *refStoreWrap) CompareAndSwap(name string, old Hash, new Hash) (bool, error) {
	return r.inner.CompareAndSwap(name, istorage.Hash(old), istorage.Hash(new))
}

func (r *refStoreWrap) DefaultBranch() (string, error)     { return r.inner.DefaultBranch() }
func (r *refStoreWrap) SetDefaultBranch(name string) error { return r.inner.SetDefaultBranch(name) }

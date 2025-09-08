package minigit

import (
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestObjectPutGet(t *testing.T) {
	reg := NewInMemoryRegistry()
	if err := reg.CreateBare("t"); err != nil {
		t.Fatal(err)
	}
	repo, ok := reg.Get("t")
	if !ok {
		t.Fatal("repo missing")
	}
	h, err := PutBlob(repo, []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	obj, err := GetObject(repo, h)
	if err != nil {
		t.Fatal(err)
	}
	if obj.Type != ObjBlob || string(obj.Data) != "hello" {
		t.Fatalf("unexpected object: %+v", obj)
	}
}

func TestWorktreeCommit(t *testing.T) {
	reg := NewInMemoryRegistry()
	if err := reg.CreateBare("w"); err != nil {
		t.Fatal(err)
	}
	repo, _ := reg.Get("w")
	wt := NewWorktree(repo)
	wt.Add("dir/file.txt", []byte("data"))
	commit, err := wt.Commit("init", "main")
	if err != nil {
		t.Fatal(err)
	}
	// Ref set
	if h, ok, _ := repo.Refs().Get("refs/heads/main"); !ok || h == "" {
		t.Fatalf("ref not set: %v %v", ok, h)
	}
	// Parse commit to find tree
	cobj, err := repo.Objects().Get(commit)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(cobj.Data), "\n")
	if !strings.HasPrefix(lines[0], "tree ") {
		t.Fatalf("bad commit header: %q", lines[0])
	}
	treeHash := Hash(strings.TrimPrefix(lines[0], "tree "))
	tobj, err := repo.Objects().Get(treeHash)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(tobj.Data, []byte("dir")) {
		t.Fatalf("tree missing dir entry: %q", string(tobj.Data))
	}
}

func TestWorktreeOverlayPreservesUnmodifiedAndDeletes(t *testing.T) {
	reg := NewInMemoryRegistry()
	if err := reg.CreateBare("ov"); err != nil {
		t.Fatal(err)
	}
	repo, _ := reg.Get("ov")
	// First commit with keep and dir/x.txt
	wt1 := NewWorktree(repo)
	wt1.Add("keep.txt", []byte("old"))
	wt1.Add("dir/x.txt", []byte("x"))
	c1, err := wt1.Commit("first", "main")
	if err != nil {
		t.Fatal(err)
	}
	// Second commit: modify keep, delete dir/x.txt, add new.txt
	wt2 := NewWorktree(repo)
	wt2.Add("keep.txt", []byte("new"))
	wt2.Remove("dir/x.txt")
	wt2.Add("new.txt", []byte("n"))
	c2, err := wt2.Commit("second", "main")
	if err != nil {
		t.Fatal(err)
	}

	// Validate parent line
	c2obj, err := repo.Objects().Get(c2)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(c2obj.Data), "parent "+string(c1)) {
		t.Fatalf("missing parent; commit: %q", string(c2obj.Data))
	}

	// Collect files from c2 tree
	treeHash, err := parseCommitTreeLine(c2obj.Data)
	if err != nil {
		t.Fatal(err)
	}
	files, err := readTreeFiles(repo, treeHash)
	if err != nil {
		t.Fatal(err)
	}
	// keep.txt present
	if _, ok := files["keep.txt"]; !ok {
		t.Fatalf("keep.txt missing in tree: %+v", files)
	}
	// new.txt present
	if _, ok := files["new.txt"]; !ok {
		t.Fatalf("new.txt missing in tree: %+v", files)
	}
	// dir/x.txt removed
	if _, ok := files["dir/x.txt"]; ok {
		t.Fatalf("dir/x.txt should be removed; have %+v", files)
	}
}

func TestCloneFromLocalBare(t *testing.T) {
	tmp := t.TempDir()
	gitdir := filepath.Join(tmp, ".git")
	if err := os.MkdirAll(filepath.Join(gitdir, "objects"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(gitdir, "refs", "heads"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Blob
	bHash, err := writeLooseObject(gitdir, "blob", []byte("hello\n"))
	if err != nil {
		t.Fatal(err)
	}
	// Tree with one file
	var treeBuf []byte
	raw, _ := hexTo20Test(bHash)
	treeBuf = append(treeBuf, []byte("100644 ")...)
	treeBuf = append(treeBuf, []byte("hello.txt")...)
	treeBuf = append(treeBuf, 0)
	treeBuf = append(treeBuf, raw[:]...)
	tHash, err := writeLooseObject(gitdir, "tree", treeBuf)
	if err != nil {
		t.Fatal(err)
	}
	// Commit
	var cbuf []byte
	cbuf = append(cbuf, []byte("tree ")...)
	cbuf = append(cbuf, []byte(tHash)...)
	cbuf = append(cbuf, '\n', '\n')
	cbuf = append(cbuf, []byte("msg\n")...)
	cHash, err := writeLooseObject(gitdir, "commit", cbuf)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitdir, "refs", "heads", "main"), []byte(cHash+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitdir, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	reg := NewInMemoryRegistry()
	if err := Clone(reg, "c", tmp); err != nil {
		t.Fatal(err)
	}
	repo, ok := reg.Get("c")
	if !ok {
		t.Fatal("cloned repo missing")
	}
	if h, ok, _ := repo.Refs().Get("refs/heads/main"); !ok || string(h) != cHash {
		t.Fatalf("ref mismatch: %v %v != %v", ok, h, cHash)
	}
	// verify commit exists
	if _, err := repo.Objects().Get(Hash(cHash)); err != nil {
		t.Fatalf("commit missing: %v", err)
	}
}

func TestWorktreeFromCommit_ReadWriteListAndParenting(t *testing.T) {
	reg := NewInMemoryRegistry()
	if err := reg.CreateBare("r"); err != nil {
		t.Fatal(err)
	}
	repo, _ := reg.Get("r")
	// initial commit
	wt1 := NewWorktree(repo)
	wt1.Add("a.txt", []byte("A"))
	wt1.Add("b.txt", []byte("B"))
	wt1.Add("dir/c.txt", []byte("C"))
	c1, err := wt1.Commit("init", "main")
	if err != nil {
		t.Fatal(err)
	}

	// worktree based on c1
	w2, err := NewWorktreeFromCommit(repo, c1)
	if err != nil {
		t.Fatal(err)
	}
	// Read existing file from base
	data, ok, err := w2.ReadFile("a.txt")
	if err != nil || !ok || string(data) != "A" {
		t.Fatalf("ReadFile a.txt: %v %v %q", err, ok, string(data))
	}
	// Stage modifications
	w2.WriteFile("b.txt", []byte("B2"))
	w2.Remove("dir/c.txt")
	w2.Add("d.txt", []byte("D"))
	// List files before commit
	files, err := w2.ListFiles()
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(files, ",")
	if !strings.Contains(joined, "a.txt") || !strings.Contains(joined, "b.txt") || !strings.Contains(joined, "d.txt") || strings.Contains(joined, "dir/c.txt") {
		t.Fatalf("ListFiles mismatch: %v", files)
	}
	// Commit to a new branch feature; should parent to c1
	c2, err := w2.Commit("feat", "feature")
	if err != nil {
		t.Fatal(err)
	}
	c2obj, err := repo.Objects().Get(c2)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(c2obj.Data), "parent "+string(c1)) {
		t.Fatalf("c2 missing parent c1: %q", string(c2obj.Data))
	}
	if h, ok, _ := repo.Refs().Get("refs/heads/feature"); !ok || h == "" {
		t.Fatalf("feature ref not set: %v %v", ok, h)
	}
}

func TestWorktreeModesAndSymlinks(t *testing.T) {
	reg := NewInMemoryRegistry()
	_ = reg.CreateBare("m")
	repo, _ := reg.Get("m")
	wt := NewWorktree(repo)
	wt.AddWithMode("exec.sh", []byte("echo hi\n"), 0o755)
	wt.AddSymlink("link.txt", "target/path.txt")
	// Stage a file in a subdirectory; directory mode is implicit as 040000
	wt.Add("dir/file.txt", []byte("x"))
	if _, err := wt.Commit("modes", "main"); err != nil {
		t.Fatal(err)
	}
	// Examine the root tree
	cref, _, _ := repo.Refs().Get("refs/heads/main")
	if cref == "" {
		t.Fatal("ref missing")
	}
	cobj, _ := repo.Objects().Get(cref)
	tree, err := parseCommitTreeLine(cobj.Data)
	if err != nil {
		t.Fatal(err)
	}
	ents, err := ListTreeEntries(repo, tree)
	if err != nil {
		t.Fatal(err)
	}
	lookup := func(name string) (TreeEntry, bool) {
		for _, e := range ents {
			if e.Path == name {
				return e, true
			}
		}
		return TreeEntry{}, false
	}
	if e, ok := lookup("exec.sh"); !ok || (e.Mode&0o111) == 0 {
		t.Fatalf("exec.sh mode mismatch: %+v", e)
	}
	if e, ok := lookup("link.txt"); !ok || (e.Mode&os.ModeSymlink) == 0 || e.Kind != "symlink" {
		t.Fatalf("link.txt mismatch: %+v", e)
	}
	if e, ok := lookup("dir"); !ok || (e.Mode&os.ModeDir) == 0 || e.Kind != "dir" {
		t.Fatalf("dir entry mismatch: %+v", e)
	}
	// Verify Readlink reports correct target for staged symlink
	if target, ok, err := wt.Readlink("link.txt"); err != nil || !ok || target != "target/path.txt" {
		t.Fatalf("Readlink wrong: %v %v %q", err, ok, target)
	}
}

func TestWorktreeSetAuthor(t *testing.T) {
	reg := NewInMemoryRegistry()
	_ = reg.CreateBare("a")
	repo, _ := reg.Get("a")
	wt := NewWorktree(repo)
	wt.SetAuthor("Alice", "alice@example.com")
	wt.Add("f.txt", []byte("x"))
	c, err := wt.Commit("msg", "main")
	if err != nil {
		t.Fatal(err)
	}
	obj, err := repo.Objects().Get(c)
	if err != nil {
		t.Fatal(err)
	}
	s := string(obj.Data)
	if !strings.Contains(s, "author Alice <alice@example.com>") {
		t.Fatalf("author not set: %q", s)
	}
	if !strings.Contains(s, "committer Alice <alice@example.com>") {
		t.Fatalf("committer not set: %q", s)
	}
}

func TestWorktreeCommitterAndTimes(t *testing.T) {
	reg := NewInMemoryRegistry()
	_ = reg.CreateBare("t")
	repo, _ := reg.Get("t")
	wt := NewWorktree(repo)
	wt.SetAuthor("Alice", "alice@example.com")
	wt.SetCommitter("Bob", "bob@example.com")
	tzA := time.FixedZone("A", 2*3600)
	tzC := time.FixedZone("C", -7*3600)
	wt.SetAuthorTime(time.Unix(1000, 0).In(tzA))
	wt.SetCommitterTime(time.Unix(2000, 0).In(tzC))
	wt.Add("f.txt", []byte("x"))
	c, err := wt.Commit("msg", "main")
	if err != nil {
		t.Fatal(err)
	}
	obj, err := repo.Objects().Get(c)
	if err != nil {
		t.Fatal(err)
	}
	s := string(obj.Data)
	if !strings.Contains(s, "author Alice <alice@example.com> 1000 +0200") {
		t.Fatalf("author line mismatch: %q", s)
	}
	if !strings.Contains(s, "committer Bob <bob@example.com> 2000 -0700") {
		t.Fatalf("committer line mismatch: %q", s)
	}
}

func writeLooseObject(gitdir, typ string, data []byte) (string, error) {
	header := fmt.Sprintf("%s %d\x00", typ, len(data))
	sum := sha1.Sum(append([]byte(header), data...))
	hex := fmt.Sprintf("%x", sum[:])
	dir := filepath.Join(gitdir, "objects", hex[:2])
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	f, err := os.Create(filepath.Join(dir, hex[2:]))
	if err != nil {
		return "", err
	}
	zw := zlib.NewWriter(f)
	if _, err := zw.Write([]byte(header)); err != nil {
		f.Close()
		return "", err
	}
	if _, err := zw.Write(data); err != nil {
		zw.Close()
		f.Close()
		return "", err
	}
	if err := zw.Close(); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return hex, nil
}

func hexTo20Test(s string) ([20]byte, error) {
	var out [20]byte
	for i := 0; i < 20; i++ {
		out[i] = (nibble(s[i*2]) << 4) | nibble(s[i*2+1])
	}
	return out, nil
}

func nibble(c byte) byte {
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

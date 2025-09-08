package minigit

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	neturl "net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tinyrange/tinyrange/vibe_party/minigit/internal/git"
)

// Clone imports a repository into the given in-memory registry under repoName.
// It supports local filesystem paths (including file://) and HTTP(S) Git smart protocol.
func Clone(reg Registry, repoName, source string) error {
	if _, ok := reg.Get(repoName); !ok {
		if err := reg.CreateBare(repoName); err != nil {
			return err
		}
	}
	// Local path
	path := source
	if strings.HasPrefix(source, "file://") {
		path = strings.TrimPrefix(source, "file://")
	}
	if st, err := os.Stat(path); err == nil && st.IsDir() {
		dir := path
		if _, err := os.Stat(filepath.Join(dir, "objects")); err != nil {
			if _, err2 := os.Stat(filepath.Join(dir, ".git", "objects")); err2 == nil {
				dir = filepath.Join(dir, ".git")
			}
		}
		_, ok := reg.Get(repoName)
		if !ok {
			return errors.New("repo missing after create")
		}
		return importBarePath(reg, repoName, dir)
	}
	// HTTP(S)
	if u, err := neturl.Parse(source); err == nil {
		if u.Scheme == "http" || u.Scheme == "https" {
			if err := seedFromHTTPv2(reg, repoName, u); err == nil {
				return nil
			}
			return seedFromHTTPv0(reg, repoName, u)
		}
	}
	return fmt.Errorf("unsupported clone source: %s", source)
}

// seedFromHTTPv0 performs a minimal Git smart HTTP v0 fetch from a remote and
// imports all advertised refs and objects into the registry.
func seedFromHTTPv0(reg Registry, repoName string, base *neturl.URL) error {
	// Discover refs
	infoURL := *base
	infoURL.Path = strings.TrimSuffix(infoURL.Path, "/") + "/info/refs"
	q := infoURL.Query()
	q.Set("service", "git-upload-pack")
	infoURL.RawQuery = q.Encode()
	resp, err := http.Get(infoURL.String())
	if err != nil {
		return fmt.Errorf("GET info/refs: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("info/refs status %d", resp.StatusCode)
	}
	br := bufio.NewReader(resp.Body)
	pr := git.NewPktReaderFromBuf(br)
	refs := make(map[string]Hash)
	var defaultBranch string
	first := true
	for {
		data, _, err := pr.ReadPacket()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if data == nil {
			continue
		}
		line := string(bytes.TrimRight(data, "\n"))
		if strings.HasPrefix(line, "# ") {
			continue
		}
		if i := strings.IndexByte(line, 0); i >= 0 {
			caps := strings.Fields(strings.ReplaceAll(line[i+1:], "\x00", ""))
			for _, c := range caps {
				const p = "symref=HEAD:"
				if strings.HasPrefix(c, p) {
					defaultBranch = strings.TrimPrefix(c, p)
				}
			}
			line = line[:i]
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		refs[parts[1]] = Hash(parts[0])
		if first {
			first = false
		}
	}
	if len(refs) == 0 {
		return errors.New("remote advertised no refs")
	}
	uniq := make(map[Hash]struct{})
	for _, h := range refs {
		if len(h) == 40 {
			uniq[h] = struct{}{}
		}
	}
	wants := make([]string, 0, len(uniq))
	for h := range uniq {
		wants = append(wants, string(h))
	}
	sort.Strings(wants)
	upURL := *base
	upURL.Path = strings.TrimSuffix(upURL.Path, "/") + "/git-upload-pack"
	var body bytes.Buffer
	pw := git.NewPktWriter(&body)
	for i, h := range wants {
		if i == 0 {
			_ = pw.WriteStringLine("want " + h + "\x00ofs-delta agent=minigit\n")
		} else {
			_ = pw.WriteStringLine("want " + h + "\n")
		}
	}
	_ = pw.WriteFlush()
	_ = pw.WriteStringLine("done\n")
	req, _ := http.NewRequest("POST", upURL.String(), bytes.NewReader(body.Bytes()))
	req.Header.Set("Content-Type", "application/x-git-upload-pack-request")
	req.Header.Set("Accept", "application/x-git-upload-pack-result")
	httpResp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer httpResp.Body.Close()
	if httpResp.StatusCode != http.StatusOK {
		return fmt.Errorf("upload-pack status %d", httpResp.StatusCode)
	}
	br2 := bufio.NewReader(httpResp.Body)
	for {
		peek, err := br2.Peek(4)
		if err != nil {
			return err
		}
		if isHex4(peek) {
			pr2 := git.NewPktReaderFromBuf(br2)
			if _, _, err := pr2.ReadPacket(); err != nil && err != io.EOF {
				return err
			}
			continue
		}
		break
	}
	rep, ok := reg.Get(repoName)
	if !ok {
		return errors.New("repo missing")
	}
	obj := rep.Objects()
	resolver := func(id [20]byte) (git.ObjType, []byte, bool) {
		hex := make([]byte, 40)
		const hd = "0123456789abcdef"
		for i, b := range id[:] {
			hex[i*2] = hd[b>>4]
			hex[i*2+1] = hd[b&0x0f]
		}
		if o, err := obj.Get(Hash(string(hex))); err == nil {
			return mapStoreToGit(ObjectType(o.Type)), o.Data, true
		}
		return 0, nil, false
	}
	if err := git.ReadPack(br2, resolver, func(ro git.RawObject) error {
		_, err := obj.Put(&Object{Type: mapGitToStore(ro.Type), Size: int64(len(ro.Data)), Data: ro.Data})
		return err
	}); err != nil {
		return err
	}
	// Set refs
	rrefs := rep.Refs()
	for name, h := range refs {
		_ = rrefs.Set(name, h)
	}
	if defaultBranch != "" {
		_ = rrefs.SetDefaultBranch(defaultBranch)
	}
	return nil
}

func seedFromHTTPv2(reg Registry, repoName string, base *neturl.URL) error {
	upURL := *base
	upURL.Path = strings.TrimSuffix(upURL.Path, "/") + "/git-upload-pack"
	var lsBody bytes.Buffer
	lsw := git.NewPktWriter(&lsBody)
	_ = lsw.WriteStringLine("command=ls-refs\n")
	_ = lsw.WriteStringLine("agent=minigit\n")
	_ = lsw.WriteStringLine("peel\n")
	_ = lsw.WriteStringLine("symrefs\n")
	_ = lsw.WriteStringLine("ref-prefix refs/heads/\n")
	_ = lsw.WriteStringLine("ref-prefix refs/tags/\n")
	_ = lsw.WriteFlush()
	req, _ := http.NewRequest("POST", upURL.String(), bytes.NewReader(lsBody.Bytes()))
	req.Header.Set("Git-Protocol", "version=2")
	req.Header.Set("Content-Type", "application/x-git-upload-pack-request")
	req.Header.Set("Accept", "application/x-git-upload-pack-result")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ls-refs status %d", resp.StatusCode)
	}
	refs := make(map[string]Hash)
	var defaultBranch string
	br := bufio.NewReader(resp.Body)
	pr := git.NewPktReaderFromBuf(br)
	for {
		data, _, err := pr.ReadPacket()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if data == nil {
			break
		}
		line := strings.TrimRight(string(data), "\n")
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		refs[f[1]] = Hash(f[0])
		for _, a := range f[2:] {
			const p = "symref-target:"
			if strings.HasPrefix(a, p) && f[1] == "HEAD" {
				defaultBranch = strings.TrimPrefix(a, p)
			}
		}
	}
	if len(refs) == 0 {
		return errors.New("v2: no refs")
	}
	uniq := make(map[Hash]struct{})
	for _, h := range refs {
		if len(h) == 40 {
			uniq[h] = struct{}{}
		}
	}
	wants := make([]string, 0, len(uniq))
	for h := range uniq {
		wants = append(wants, string(h))
	}
	sort.Strings(wants)
	var fetchBody bytes.Buffer
	fw := git.NewPktWriter(&fetchBody)
	_ = fw.WriteStringLine("command=fetch\n")
	_ = fw.WriteStringLine("agent=minigit\n")
	_ = fw.WriteFlush()
	for _, w := range wants {
		_ = fw.WriteStringLine("want " + w + "\n")
	}
	_ = fw.WriteStringLine("ofs-delta\n")
	_ = fw.WriteStringLine("done\n")
	_ = fw.WriteFlush()
	freq, _ := http.NewRequest("POST", upURL.String(), bytes.NewReader(fetchBody.Bytes()))
	freq.Header.Set("Git-Protocol", "version=2")
	freq.Header.Set("Content-Type", "application/x-git-upload-pack-request")
	freq.Header.Set("Accept", "application/x-git-upload-pack-result")
	fresp, err := http.DefaultClient.Do(freq)
	if err != nil {
		return err
	}
	defer fresp.Body.Close()
	if fresp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch status %d", fresp.StatusCode)
	}
	br2 := bufio.NewReader(fresp.Body)
	pr2 := git.NewPktReaderFromBuf(br2)
	for {
		data, kind, err := pr2.ReadPacket()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if kind == -1 {
			continue
		}
		if data == nil {
			continue
		}
		if string(bytes.TrimRight(data, "\n")) == "packfile" {
			if _, kind, _ := pr2.ReadPacket(); kind != -1 { /* ignore */
			}
			break
		}
	}
	rep, ok := reg.Get(repoName)
	if !ok {
		return errors.New("repo missing")
	}
	obj := rep.Objects()
	resolver := func(id [20]byte) (git.ObjType, []byte, bool) {
		hex := make([]byte, 40)
		const hd = "0123456789abcdef"
		for i, b := range id[:] {
			hex[i*2] = hd[b>>4]
			hex[i*2+1] = hd[b&0x0f]
		}
		if o, err := obj.Get(Hash(string(hex))); err == nil {
			return mapStoreToGit(ObjectType(o.Type)), o.Data, true
		}
		return 0, nil, false
	}
	if err := git.ReadPack(br2, resolver, func(ro git.RawObject) error {
		_, err := obj.Put(&Object{Type: mapGitToStore(ro.Type), Size: int64(len(ro.Data)), Data: ro.Data})
		return err
	}); err != nil {
		return err
	}
	rrefs := rep.Refs()
	for name, h := range refs {
		_ = rrefs.Set(name, h)
	}
	if defaultBranch != "" {
		_ = rrefs.SetDefaultBranch(defaultBranch)
	}
	return nil
}

func isHex4(b []byte) bool {
	if len(b) < 4 {
		return false
	}
	for i := 0; i < 4; i++ {
		c := b[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// importBarePath imports a repository from a local .git (bare) directory.
func importBarePath(reg Registry, repoName, dir string) error {
	rep, ok := reg.Get(repoName)
	if !ok {
		return errors.New("repo missing")
	}
	objStore := rep.Objects()
	// Pack files
	packs, _ := filepath.Glob(filepath.Join(dir, "objects", "pack", "*.pack"))
	for _, p := range packs {
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		br := bufio.NewReader(f)
		resolver := func(id [20]byte) (git.ObjType, []byte, bool) { return 0, nil, false }
		if err := git.ReadPack(br, resolver, func(o git.RawObject) error {
			_, err := objStore.Put(&Object{Type: mapGitToStore(o.Type), Size: int64(len(o.Data)), Data: o.Data})
			return err
		}); err != nil {
			f.Close()
			return err
		}
		f.Close()
	}
	// Loose objects
	root := filepath.Join(dir, "objects")
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		if len(rel) != 41 || rel[2] != '/' {
			return nil
		}
		f, err := os.Open(p)
		if err != nil {
			return nil
		}
		zr, err := zlib.NewReader(f)
		if err != nil {
			f.Close()
			return nil
		}
		br := bufio.NewReader(zr)
		header, err := br.ReadString('\x00')
		if err != nil {
			zr.Close()
			f.Close()
			return nil
		}
		header = header[:len(header)-1]
		parts := strings.SplitN(header, " ", 2)
		if len(parts) != 2 {
			zr.Close()
			f.Close()
			return nil
		}
		var typ ObjectType
		switch parts[0] {
		case "commit":
			typ = ObjCommit
		case "tree":
			typ = ObjTree
		case "blob":
			typ = ObjBlob
		case "tag":
			typ = ObjTag
		default:
			typ = ObjInvalid
		}
		data, _ := io.ReadAll(br)
		zr.Close()
		f.Close()
		if typ != ObjInvalid {
			_, _ = objStore.Put(&Object{Type: typ, Size: int64(len(data)), Data: data})
		}
		return nil
	})
	// Refs
	refs := rep.Refs()
	if b, err := os.ReadFile(filepath.Join(dir, "HEAD")); err == nil {
		line := strings.TrimSpace(string(b))
		const pfx = "ref: "
		if strings.HasPrefix(line, pfx) {
			_ = refs.SetDefaultBranch(strings.TrimPrefix(line, pfx))
		}
	}
	if b, err := os.ReadFile(filepath.Join(dir, "packed-refs")); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "^") {
				continue
			}
			f := strings.Fields(line)
			if len(f) >= 2 {
				_ = refs.Set(f[1], Hash(f[0]))
			}
		}
	}
	reffolder := filepath.Join(dir, "refs")
	filepath.WalkDir(reffolder, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		hash := strings.TrimSpace(string(b))
		if len(hash) != 40 {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		_ = refs.Set(filepath.ToSlash(rel), Hash(hash))
		return nil
	})
	return nil
}

func mapGitToStore(t git.ObjType) ObjectType {
	switch t {
	case git.ObjCommit:
		return ObjCommit
	case git.ObjTree:
		return ObjTree
	case git.ObjBlob:
		return ObjBlob
	case git.ObjTag:
		return ObjTag
	default:
		return ObjInvalid
	}
}
func mapStoreToGit(t ObjectType) git.ObjType {
	switch t {
	case ObjCommit:
		return git.ObjCommit
	case ObjTree:
		return git.ObjTree
	case ObjBlob:
		return git.ObjBlob
	case ObjTag:
		return git.ObjTag
	default:
		return git.ObjBad
	}
}

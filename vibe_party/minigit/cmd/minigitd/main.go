package main

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	neturl "net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tinyrange/tinyrange/vibe_party/minigit/internal/git"
	"github.com/tinyrange/tinyrange/vibe_party/minigit/internal/server"
	"github.com/tinyrange/tinyrange/vibe_party/minigit/internal/storage"
	"github.com/tinyrange/tinyrange/vibe_party/minigit/internal/storage/memstore"
)

func main() {
	addr := flag.String("addr", ":8080", "HTTP listen address")
	reposFlag := flag.String("repos", "", "Comma-separated list of repo names to create in-memory (e.g. 'alpha,beta')")
	seedsFlag := flag.String("seed", "", "Comma-separated repo=url pairs to seed at startup (e.g. 'demo=https://github.com/user/repo.git')")
	flag.Parse()

	// Initialize in-memory repo registry
	reg := memstore.NewRegistry()

	if *reposFlag != "" {
		for _, name := range strings.Split(*reposFlag, ",") {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			if err := reg.CreateBare(name); err != nil {
				log.Fatalf("create repo %q: %v", name, err)
			}
		}
	}

	if *seedsFlag != "" {
		pairs := parseSeeds(*seedsFlag)
		for repo, url := range pairs {
			if _, ok := reg.Get(repo); !ok {
				if err := reg.CreateBare(repo); err != nil {
					log.Fatalf("create seed repo %q: %v", repo, err)
				}
			}
			if err := seedRepoFromURL(reg, repo, url); err != nil {
				log.Fatalf("seed %s from %s: %v", repo, url, err)
			}
		}
	}

	mux := http.NewServeMux()

	// Basic health endpoint
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

	// Git smart HTTP v2 endpoints
	gitHandler, err := server.NewHTTP(reg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "init server error: %v\n", err)
		os.Exit(1)
	}
	mux.Handle("/", gitHandler)

	log.Printf("minigitd listening on %s", *addr)
	if err := http.ListenAndServe(*addr, mux); err != nil {
		log.Fatalf("listen: %v", err)
	}
}

func parseSeeds(s string) map[string]string {
	m := make(map[string]string)
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		eq := strings.IndexByte(part, '=')
		if eq <= 0 {
			continue
		}
		repo := strings.TrimSpace(part[:eq])
		url := strings.TrimSpace(part[eq+1:])
		if repo != "" && url != "" {
			m[repo] = url
		}
	}
	return m
}

func seedRepoFromURL(reg *memstore.Registry, repoName, url string) error {
	log.Printf("seed %s: start from %s", repoName, url)
	// Try local filesystem path first (including file://).
	path := url
	if strings.HasPrefix(url, "file://") {
		path = strings.TrimPrefix(url, "file://")
	}
	if st, err := os.Stat(path); err == nil && st.IsDir() {
		dir := path
		if _, err := os.Stat(filepath.Join(dir, "objects")); err != nil {
			if _, err2 := os.Stat(filepath.Join(dir, ".git", "objects")); err2 == nil {
				dir = filepath.Join(dir, ".git")
			}
		}
		log.Printf("seed %s: importing local repo at %s", repoName, dir)
		err := importBarePath(reg, repoName, dir)
		if err == nil {
			log.Printf("seed %s: done (local)", repoName)
		}
		return err
	}

	// Next, support http(s) Git smart protocol. Try v2 first, then fall back to v0.
	if u, err := neturl.Parse(url); err == nil {
		if u.Scheme == "http" || u.Scheme == "https" {
			if err := seedFromHTTPv2(reg, repoName, u); err == nil {
				return nil
			} else {
				log.Printf("v2 seed failed for %s: %v; falling back to v0", url, err)
			}
			return seedFromHTTPv0(reg, repoName, u)
		}
	}

	return fmt.Errorf("unsupported seed URL: %s", url)
}

// seedFromHTTPv0 performs a minimal Git smart HTTP v0 fetch from a remote and
// imports all advertised refs and objects into the in-memory registry.
func seedFromHTTPv0(reg *memstore.Registry, repoName string, base *neturl.URL) error {
	log.Printf("seed %s: using HTTP v0 from %s", repoName, base.String())
	// Step 1: Discover refs via info/refs?service=git-upload-pack
	infoURL := *base
	// Ensure path ends without trailing slash issues
	if !strings.HasSuffix(infoURL.Path, "/") {
		// leave as-is; we append /info/refs
	}
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
	refs := make(map[string]storage.Hash)
	var defaultBranch string
	// Some servers start with a banner pkt then flush; be tolerant.
	// Read packets until flush; skip banner lines starting with '#'.
	firstRef := true
	for {
		data, _, err := pr.ReadPacket()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read info/refs pkt: %w", err)
		}
		if data == nil {
			// flush; after the service header, many servers flush once and then emit refs
			// We'll continue reading subsequent packets for refs until second flush or EOF.
			// Continue loop to read more packets.
			// If the server ended here, subsequent ReadPacket will hit EOF and break.
			continue
		}
		line := string(bytes.TrimRight(data, "\n"))
		if strings.HasPrefix(line, "# ") {
			// banner like "# service=git-upload-pack"
			continue
		}
		// First ref line may include capabilities after NUL
		if i := strings.IndexByte(line, 0); i >= 0 {
			// Extract capabilities for symref (HEAD -> default branch)
			caps := strings.Fields(strings.ReplaceAll(line[i+1:], "\x00", ""))
			for _, c := range caps {
				const pfx = "symref=HEAD:"
				if strings.HasPrefix(c, pfx) {
					defaultBranch = strings.TrimPrefix(c, pfx)
				}
			}
			line = line[:i]
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		hashHex := fields[0]
		name := fields[1]
		refs[name] = storage.Hash(hashHex)
		// Some servers include peeled tags `^{};` already separate entries; just record as-is.
		if firstRef {
			firstRef = false
		}
	}

	if len(refs) == 0 {
		return errors.New("remote advertised no refs")
	}
	log.Printf("seed %s (v0): discovered %d refs%s", repoName, len(refs), func() string {
		if defaultBranch != "" {
			return "; HEAD->" + defaultBranch
		}
		return ""
	}())

	// Build wants: include all advertised object IDs (unique)
	uniq := make(map[storage.Hash]struct{})
	for _, h := range refs {
		if len(h) == 40 { // basic sanity
			uniq[h] = struct{}{}
		}
	}
	wants := make([]string, 0, len(uniq))
	for h := range uniq {
		wants = append(wants, string(h))
	}
	sort.Strings(wants)

	// Step 2: Request pack via POST to git-upload-pack using v0
	upURL := *base
	upURL.Path = strings.TrimSuffix(upURL.Path, "/") + "/git-upload-pack"
	var reqBody bytes.Buffer
	pw := git.NewPktWriter(&reqBody)
	for i, h := range wants {
		if i == 0 {
			// Include minimal capabilities on first want. Avoid thin-pack/side-band.
			_ = pw.WriteStringLine("want " + h + "\x00ofs-delta agent=minigit\n")
		} else {
			_ = pw.WriteStringLine("want " + h + "\n")
		}
	}
	// End wants section
	_ = pw.WriteFlush()
	// No 'have' lines; request pack immediately
	_ = pw.WriteStringLine("done\n")

	httpReq, err := http.NewRequest("POST", upURL.String(), bytes.NewReader(reqBody.Bytes()))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/x-git-upload-pack-request")
	httpReq.Header.Set("Accept", "application/x-git-upload-pack-result")

	httpResp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("POST git-upload-pack: %w", err)
	}
	defer httpResp.Body.Close()
	if httpResp.StatusCode != http.StatusOK {
		return fmt.Errorf("upload-pack status %d", httpResp.StatusCode)
	}

	// Server may send pkt-line framed status (NAK/ACK, progress). Consume
	// all pkt-lines up front; pack starts when next 4 bytes are not hex.
	br2 := bufio.NewReader(httpResp.Body)
	for {
		peek, err := br2.Peek(4)
		if err != nil {
			return fmt.Errorf("peek before pack: %w", err)
		}
		if isHex4(peek) {
			pr2 := git.NewPktReaderFromBuf(br2)
			if _, _, err := pr2.ReadPacket(); err != nil {
				if err == io.EOF {
					break
				}
				return fmt.Errorf("read pre-pack pkt: %w", err)
			}
			// loop to consume more pkt-lines
			continue
		}
		break
	}

	// Step 3: Decode pack into object store
	rep, ok := reg.Get(repoName)
	if !ok {
		return errors.New("repo missing")
	}
	objStore := rep.Objects()
	resolver := func(id [20]byte) (git.ObjType, []byte, bool) {
		// Try current store for REF_DELTA bases
		hex := make([]byte, 40)
		const hexdigits = "0123456789abcdef"
		for i, b := range id[:] {
			hex[i*2] = hexdigits[b>>4]
			hex[i*2+1] = hexdigits[b&0x0f]
		}
		if obj, err := objStore.Get(storage.Hash(string(hex))); err == nil {
			return mapStoreToGit(obj.Type), obj.Data, true
		}
		return 0, nil, false
	}
	var objCount int
	lastLog := time.Now()
	if err := git.ReadPack(br2, resolver, func(ro git.RawObject) error {
		_, err := objStore.Put(&storage.Object{Type: mapGitToStore(ro.Type), Size: int64(len(ro.Data)), Data: ro.Data})
		objCount++
		if objCount%200 == 0 || time.Since(lastLog) > 2*time.Second {
			log.Printf("seed %s (v0): imported %d objects...", repoName, objCount)
			lastLog = time.Now()
		}
		return err
	}); err != nil {
		return fmt.Errorf("read remote pack: %w", err)
	}
	log.Printf("seed %s (v0): pack import complete (%d objects)", repoName, objCount)

	// Step 4: Set refs and default branch
	rrefs := rep.Refs()
	for name, h := range refs {
		_ = rrefs.Set(name, h)
	}
	if defaultBranch != "" {
		_ = rrefs.SetDefaultBranch(defaultBranch)
	} else {
		// Heuristic fallback
		if _, ok := refs["refs/heads/main"]; ok {
			_ = rrefs.SetDefaultBranch("refs/heads/main")
		} else if _, ok := refs["refs/heads/master"]; ok {
			_ = rrefs.SetDefaultBranch("refs/heads/master")
		}
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

// -------- v2 HTTP client --------

func seedFromHTTPv2(reg *memstore.Registry, repoName string, base *neturl.URL) error {
	log.Printf("seed %s: using HTTP v2 from %s", repoName, base.String())
	// 1) ls-refs
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

	req, err := http.NewRequest("POST", upURL.String(), bytes.NewReader(lsBody.Bytes()))
	if err != nil {
		return err
	}
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
	refs := make(map[string]storage.Hash)
	var defaultBranch string
	br := bufio.NewReader(resp.Body)
	pr := git.NewPktReaderFromBuf(br)
	for {
		data, _, err := pr.ReadPacket()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("ls-refs read: %w", err)
		}
		if data == nil { // flush
			break
		}
		line := strings.TrimRight(string(data), "\n")
		// Format: "<oid> <ref> [attr ...]"
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		oid := fields[0]
		ref := fields[1]
		refs[ref] = storage.Hash(oid)
		// attrs like "symref-target:<ref>" or "peeled:<oid>"
		for _, f := range fields[2:] {
			const sp = "symref-target:"
			if strings.HasPrefix(f, sp) && ref == "HEAD" {
				defaultBranch = strings.TrimPrefix(f, sp)
			}
		}
	}
	if len(refs) == 0 {
		return errors.New("v2: no refs")
	}
	log.Printf("seed %s (v2): discovered %d refs%s", repoName, len(refs), func() string {
		if defaultBranch != "" {
			return "; HEAD->" + defaultBranch
		}
		return ""
	}())

	// 2) fetch pack with wants
	uniq := make(map[storage.Hash]struct{})
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
	_ = fw.WriteFlush() // end of command section
	for _, w := range wants {
		_ = fw.WriteStringLine("want " + w + "\n")
	}
	_ = fw.WriteStringLine("ofs-delta\n")
	_ = fw.WriteStringLine("done\n")
	_ = fw.WriteFlush()

	freq, err := http.NewRequest("POST", upURL.String(), bytes.NewReader(fetchBody.Bytes()))
	if err != nil {
		return err
	}
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
	sawPack := false
	for {
		data, kind, err := pr2.ReadPacket()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("fetch pkt: %w", err)
		}
		if kind == -1 { // delim
			// Usually appears right before the packfile
			continue
		}
		if data == nil { // flush
			continue
		}
		if string(bytes.TrimRight(data, "\n")) == "packfile" {
			sawPack = true
			// Next expected packet is a delim separating headers from raw pack
			if _, kind, _ := pr2.ReadPacket(); kind != -1 {
				// some servers may emit progress/info; tolerate
			}
			break
		}
		// else: ignore other sections (acknowledgments, shallow-info, etc.)
	}
	if !sawPack {
		return errors.New("v2: no packfile section")
	}

	// Decode raw pack starting at current position
	rep, ok := reg.Get(repoName)
	if !ok {
		return errors.New("repo missing")
	}
	objStore := rep.Objects()
	resolver := func(id [20]byte) (git.ObjType, []byte, bool) {
		hex := make([]byte, 40)
		const hexdigits = "0123456789abcdef"
		for i, b := range id[:] {
			hex[i*2] = hexdigits[b>>4]
			hex[i*2+1] = hexdigits[b&0x0f]
		}
		if obj, err := objStore.Get(storage.Hash(string(hex))); err == nil {
			return mapStoreToGit(obj.Type), obj.Data, true
		}
		return 0, nil, false
	}
	var objCount int
	lastLog := time.Now()
	if err := git.ReadPack(br2, resolver, func(ro git.RawObject) error {
		_, err := objStore.Put(&storage.Object{Type: mapGitToStore(ro.Type), Size: int64(len(ro.Data)), Data: ro.Data})
		objCount++
		if objCount%200 == 0 || time.Since(lastLog) > 2*time.Second {
			log.Printf("seed %s (v2): imported %d objects...", repoName, objCount)
			lastLog = time.Now()
		}
		return err
	}); err != nil {
		return fmt.Errorf("read v2 pack: %w", err)
	}
	log.Printf("seed %s (v2): pack import complete (%d objects)", repoName, objCount)

	// Apply refs and default branch
	rrefs := rep.Refs()
	for name, h := range refs {
		_ = rrefs.Set(name, h)
	}
	if defaultBranch != "" {
		_ = rrefs.SetDefaultBranch(defaultBranch)
	}
	return nil
}

func importBarePath(reg *memstore.Registry, repoName, dir string) error {
	rep, ok := reg.Get(repoName)
	if !ok {
		return errors.New("repo missing")
	}
	objStore := rep.Objects()
	// Import pack files first
	packs, _ := filepath.Glob(filepath.Join(dir, "objects", "pack", "*.pack"))
	for _, p := range packs {
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		resolver := func(id [20]byte) (git.ObjType, []byte, bool) {
			// Try current store
			hex := make([]byte, 40)
			const hexdigits = "0123456789abcdef"
			for i, b := range id[:] {
				hex[i*2] = hexdigits[b>>4]
				hex[i*2+1] = hexdigits[b&0x0f]
			}
			if obj, err := objStore.Get(storage.Hash(string(hex))); err == nil {
				return mapStoreToGit(obj.Type), obj.Data, true
			}
			return 0, nil, false
		}
		err = git.ReadPack(f, resolver, func(ro git.RawObject) error {
			_, err := objStore.Put(&storage.Object{Type: mapGitToStore(ro.Type), Size: int64(len(ro.Data)), Data: ro.Data})
			return err
		})
		f.Close()
		if err != nil {
			return fmt.Errorf("read pack %s: %w", p, err)
		}
	}
	// Import loose objects
	root := filepath.Join(dir, "objects")
	filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if len(rel) != 41 || rel[2] != '/' {
			return nil
		}
		// Looks like xx/yyyy...
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		zr, err := zlib.NewReader(f)
		if err != nil {
			f.Close()
			return nil
		}
		br := bufio.NewReader(zr)
		// Parse header: "type size\x00"
		header, err := br.ReadString('\x00')
		if err != nil {
			zr.Close()
			f.Close()
			return nil
		}
		header = header[:len(header)-1]
		fields := strings.SplitN(header, " ", 2)
		if len(fields) != 2 {
			zr.Close()
			f.Close()
			return nil
		}
		var typ storage.ObjectType
		switch fields[0] {
		case "commit":
			typ = storage.ObjCommit
		case "tree":
			typ = storage.ObjTree
		case "blob":
			typ = storage.ObjBlob
		case "tag":
			typ = storage.ObjTag
		default:
			typ = storage.ObjInvalid
		}
		data, _ := io.ReadAll(br)
		zr.Close()
		f.Close()
		if typ != storage.ObjInvalid {
			_, _ = objStore.Put(&storage.Object{Type: typ, Size: int64(len(data)), Data: data})
		}
		return nil
	})
	// Import refs
	refs := rep.Refs()
	// HEAD default branch
	if b, err := os.ReadFile(filepath.Join(dir, "HEAD")); err == nil {
		line := strings.TrimSpace(string(b))
		const pfx = "ref: "
		if strings.HasPrefix(line, pfx) {
			_ = refs.SetDefaultBranch(strings.TrimPrefix(line, pfx))
		}
	}
	// packed-refs
	if b, err := os.ReadFile(filepath.Join(dir, "packed-refs")); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "^") {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				_ = refs.Set(fields[1], storage.Hash(fields[0]))
			}
		}
	}
	// loose refs files
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
		_ = refs.Set(filepath.ToSlash(rel), storage.Hash(hash))
		return nil
	})
	return nil
}

func mapGitToStore(t git.ObjType) storage.ObjectType {
	switch t {
	case git.ObjCommit:
		return storage.ObjCommit
	case git.ObjTree:
		return storage.ObjTree
	case git.ObjBlob:
		return storage.ObjBlob
	case git.ObjTag:
		return storage.ObjTag
	default:
		return storage.ObjInvalid
	}
}

func mapStoreToGit(t storage.ObjectType) git.ObjType {
	switch t {
	case storage.ObjCommit:
		return git.ObjCommit
	case storage.ObjTree:
		return git.ObjTree
	case storage.ObjBlob:
		return git.ObjBlob
	case storage.ObjTag:
		return git.ObjTag
	default:
		return git.ObjBad
	}
}

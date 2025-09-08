package server

import (
	"bufio"
	"io"
	"log"
	"net/http"
	"path"
	"strings"

	"github.com/tinyrange/minigit/internal/git"
	"github.com/tinyrange/minigit/internal/storage"
)

// HTTP server implementing minimal Git smart protocol (v2 focus) without deps.

type Server struct{ reg storage.Registry }

func NewHTTP(reg storage.Registry) (http.Handler, error) {
	return &Server{reg: reg}, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	clean := path.Clean(r.URL.Path)
	if clean == "/" {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "minigit ready\n")
		return
	}
	parts := strings.Split(strings.TrimPrefix(clean, "/"), "/")
	if len(parts) < 2 {
		http.NotFound(w, r)
		return
	}
    repo := parts[0]
    // Accept common ".git" suffix in the first path segment
    if strings.HasSuffix(repo, ".git") {
        repo = strings.TrimSuffix(repo, ".git")
    }
	action := strings.Join(parts[1:], "/")
	_, ok := s.reg.Get(repo)
	if !ok {
		http.NotFound(w, r)
		return
	}

	// v2 if client sends Git-Protocol header
	if r.Header.Get("Git-Protocol") == "version=2" {
		switch action {
		case "git-upload-pack":
			s.handleUploadPackV2(repo, w, r)
			return
		case "git-receive-pack":
			s.handleReceivePackV2(repo, w, r)
			return
		}
	}

	// Minimal v0 fallback endpoints
	switch action {
	case "info/refs":
		s.handleInfoRefsV0(repo, w, r)
		return
	case "git-upload-pack":
		s.handleUploadPackV0(repo, w, r)
		return
	case "git-receive-pack":
		s.handleReceivePackV0(repo, w, r)
		return
	default:
		http.NotFound(w, r)
	}
}

// ---------- v2 ----------

func (s *Server) handleUploadPackV2(repo string, w http.ResponseWriter, r *http.Request) {
	// Parse single-command body
	br := bufio.NewReader(r.Body)
	pr := git.NewPktReaderFromBuf(br)
	var first []byte
	var err error
	if first, _, err = pr.ReadPacket(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	cmd := string(first)
	// Read until flush, ignore args
	for {
		_, _, err = pr.ReadPacket()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		// stop at flush
		// ReadPacket returns (nil,0,nil) for flush, so continue until EOF
	}

	pw := git.NewPktWriter(w)
	switch {
	case strings.HasPrefix(cmd, "command=ls-refs"):
		w.Header().Set("Content-Type", "application/x-git-upload-pack-result")
		s.writeRefs(repo, pw)
		_ = pw.WriteFlush()
	case strings.HasPrefix(cmd, "command=fetch"):
		w.Header().Set("Content-Type", "application/x-git-upload-pack-result")
		// Minimal: send a pack containing all objects
		// We don't implement acknowledgments/shallow-info; clients accept packfile only.
		// Start packfile section header for v2
		_ = pw.WriteStringLine("packfile\n")
		_ = pw.WriteDelim()
		s.writeFullPack(repo, w)
	default:
		http.Error(w, "unsupported v2 upload-pack command", 400)
	}
}

func (s *Server) handleReceivePackV2(repo string, w http.ResponseWriter, r *http.Request) {
	br := bufio.NewReader(r.Body)
	pr := git.NewPktReaderFromBuf(br)
	// Expect command=push
	pkt, _, err := pr.ReadPacket()
	if err != nil {
		log.Printf("receive-pack v2: read first pkt: %v", err)
		http.Error(w, err.Error(), 400)
		return
	}
	if !strings.HasPrefix(string(pkt), "command=push") {
		log.Printf("receive-pack v2: first pkt not command=push: %q", string(pkt))
		http.Error(w, "expected command=push", 400)
		return
	}
	// Parse updates until we see 'packfile' section header.
	var updates [][2]string // ref,new
	sawPackfile := false
	for {
		data, kind, err := pr.ReadPacket()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Printf("receive-pack v2: read pkt: %v", err)
			http.Error(w, err.Error(), 400)
			return
		}
		if kind == -1 {
			// delim packet – separates sections; ignore here
			continue
		}
		if data == nil {
			// flush – end of section
			break
		}
		line := strings.TrimRight(string(data), "\n")
		if line == "packfile" {
			sawPackfile = true
			break
		}
		if strings.HasPrefix(line, "update ") {
			// update <ref> <old> <new>
			fields := strings.Fields(line)
			if len(fields) >= 4 {
				updates = append(updates, [2]string{fields[1], fields[3]})
			}
		}
	}
	// After 'packfile' header, protocol sends a delim, then raw pack bytes until EOF.
	if sawPackfile {
		if _, kind, _ := pr.ReadPacket(); kind == -1 {
			// consumed delim
		}
	}
	// The remaining body is the packfile stream
	rep, _ := s.reg.Get(repo)
	objStore := rep.Objects()
	// Unpack into object store
	resolver := func(id [20]byte) (git.ObjType, []byte, bool) {
		// hex-encode id and load from store
		hex := make([]byte, 40)
		const hexdigits = "0123456789abcdef"
		for i, b := range id[:] {
			hex[i*2] = hexdigits[b>>4]
			hex[i*2+1] = hexdigits[b&0x0f]
		}
		if obj, err := objStore.Get(storage.Hash(string(hex))); err == nil {
			return toGitType(obj.Type), obj.Data, true
		}
		return 0, nil, false
	}
	err = git.ReadPack(br, resolver, func(o git.RawObject) error {
		t := toStoreType(o.Type)
		_, err := objStore.Put(&storage.Object{Type: t, Size: int64(len(o.Data)), Data: o.Data})
		return err
	})
	if err != nil {
		log.Printf("receive-pack v2: read pack: %v", err)
		http.Error(w, err.Error(), 400)
		return
	}

	// Update refs
	refs := rep.Refs()
	for _, u := range updates {
		_ = refs.Set(u[0], storage.Hash(u[1]))
	}

	// v2 push result: minimally send status-ok
	pw := git.NewPktWriter(w)
	w.Header().Set("Content-Type", "application/x-git-receive-pack-result")
	_ = pw.WriteStringLine("ok\n")
	_ = pw.WriteFlush()
}

// ---------- v0 fallback (very minimal) ----------

func (s *Server) handleInfoRefsV0(repo string, w http.ResponseWriter, r *http.Request) {
	service := r.URL.Query().Get("service")
	if service != "git-upload-pack" && service != "git-receive-pack" {
		http.Error(w, "bad service", 400)
		return
	}
	w.Header().Set("Content-Type", "application/x-"+service+"-advertisement")
	pw := git.NewPktWriter(w)
	_ = pw.WriteStringLine("# service=" + service + "\n")
	_ = pw.WriteFlush()
	// Advertise refs; include minimal capabilities on first ref for v0 smart protocol.
	rep, _ := s.reg.Get(repo)
	refmap, _ := rep.Refs().List()
	first := true
	for name, h := range refmap {
		if first {
			if service == "git-receive-pack" {
				// advertise minimal capabilities
				_ = pw.WriteStringLine(string(h) + " " + name + "\x00report-status\n")
			} else {
				_ = pw.WriteStringLine(string(h) + " " + name + "\n")
			}
			first = false
			continue
		}
		_ = pw.WriteStringLine(string(h) + " " + name + "\n")
	}
	_ = pw.WriteFlush()
}

func (s *Server) handleUploadPackV0(repo string, w http.ResponseWriter, r *http.Request) {
	// Client sends want/have; we ignore and return full pack after NAK
	w.Header().Set("Content-Type", "application/x-git-upload-pack-result")
	// Drain request body
	_, _ = io.Copy(io.Discard, r.Body)
	pw := git.NewPktWriter(w)
	_ = pw.WriteStringLine("NAK\n")
	s.writeFullPack(repo, w)
}

func (s *Server) handleReceivePackV0(repo string, w http.ResponseWriter, r *http.Request) {
	// v0 receive-pack: pkt-line list of updates, flush, then raw pack bytes
	br := bufio.NewReader(r.Body)
	pr := git.NewPktReaderFromBuf(br)
	var updates [][3]string // old,new,ref
	for {
		data, _, err := pr.ReadPacket()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Printf("receive-pack v0: read pkt: %v", err)
			http.Error(w, err.Error(), 400)
			return
		}
		if data == nil { // flush
			break
		}
		line := string(data)
		if i := strings.IndexByte(line, 0); i >= 0 { // strip capabilities
			line = line[:i]
		}
		fields := strings.Fields(line)
		if len(fields) >= 3 {
			updates = append(updates, [3]string{fields[0], fields[1], fields[2]})
		}
	}

	rep, _ := s.reg.Get(repo)
	objStore := rep.Objects()
	// Resolver for REF_DELTAs pointing to existing objects
	resolver := func(id [20]byte) (git.ObjType, []byte, bool) {
		hex := make([]byte, 40)
		const hexdigits = "0123456789abcdef"
		for i, b := range id[:] {
			hex[i*2] = hexdigits[b>>4]
			hex[i*2+1] = hexdigits[b&0x0f]
		}
		if obj, err := objStore.Get(storage.Hash(string(hex))); err == nil {
			return toGitType(obj.Type), obj.Data, true
		}
		return 0, nil, false
	}
	if err := git.ReadPack(br, resolver, func(o git.RawObject) error {
		t := toStoreType(o.Type)
		_, err := objStore.Put(&storage.Object{Type: t, Size: int64(len(o.Data)), Data: o.Data})
		return err
	}); err != nil {
		log.Printf("receive-pack v0: read pack: %v", err)
		http.Error(w, err.Error(), 400)
		return
	}

	// Update refs to new ids
	refs := rep.Refs()
	for _, u := range updates {
		// u[0]=old, u[1]=new, u[2]=ref
		_ = refs.Set(u[2], storage.Hash(u[1]))
	}

	w.Header().Set("Content-Type", "application/x-git-receive-pack-result")
	pw := git.NewPktWriter(w)
	_ = pw.WriteStringLine("unpack ok\n")
	for _, u := range updates {
		_ = pw.WriteStringLine("ok " + u[2] + "\n")
	}
	_ = pw.WriteFlush()
}

// ---------- helpers ----------

func (s *Server) writeRefs(repo string, pw *git.PktWriter) {
	rep, _ := s.reg.Get(repo)
	refs, _ := rep.Refs().List()
	// Deterministic order not required; clients accept arbitrary
	for name, h := range refs {
		_ = pw.WriteStringLine(string(h) + " " + name + "\n")
	}
}

func (s *Server) writeFullPack(repo string, w io.Writer) {
	rep, _ := s.reg.Get(repo)
	objs := make([]git.RawObject, 0, 64)
	_ = rep.Objects().Iter(func(h storage.Hash, t storage.ObjectType, size int64) error {
		dataObj, err := rep.Objects().Get(h)
		if err != nil {
			return err
		}
		objs = append(objs, git.RawObject{Type: toGitType(t), Data: dataObj.Data})
		return nil
	})
	if err := git.WritePack(w, objs); err != nil {
		log.Printf("write pack: %v", err)
	}
}

func toGitType(t storage.ObjectType) git.ObjType {
	switch t {
	case storage.ObjCommit:
		return git.ObjCommit
	case storage.ObjTree:
		return git.ObjTree
	case storage.ObjBlob:
		return git.ObjBlob
	case storage.ObjTag:
		return git.ObjTag
	}
	return git.ObjBad
}

func toStoreType(t git.ObjType) storage.ObjectType {
	switch t {
	case git.ObjCommit:
		return storage.ObjCommit
	case git.ObjTree:
		return storage.ObjTree
	case git.ObjBlob:
		return storage.ObjBlob
	case git.ObjTag:
		return storage.ObjTag
	}
	return storage.ObjInvalid
}

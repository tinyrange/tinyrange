package http

import (
	"bytes"
	httpstd "net/http"
	"net/http/httptest"
	"testing"

	"github.com/tinyrange/tinyrange/build/internal/common"
	"github.com/tinyrange/tinyrange/build/proto"
	"google.golang.org/protobuf/encoding/protojson"
	gproto "google.golang.org/protobuf/proto"
)

// fakeArtifact implements common.Artifact for testing.
type fakeArtifact struct {
	receipt *proto.BuildReceipt
	recErr  error
}

func (f *fakeArtifact) Open(common.FileType) (common.File, error) { return nil, nil }
func (f *fakeArtifact) Definition() (*proto.Definition, error)    { return nil, nil }
func (f *fakeArtifact) Receipt() (*proto.BuildReceipt, error)     { return f.receipt, f.recErr }

// fakeDB implements common.Database for testing.
type fakeDB struct {
	got *proto.BuildClosure
	art common.Artifact
	err error
}

// GetBuilders implements common.Database.
func (f *fakeDB) GetBuilders() ([]common.BuilderMetadata, error) {
	panic("unimplemented")
}

func (f *fakeDB) Factory() common.Factory { return nil }
func (f *fakeDB) Build(def common.BuildClosure, opt ...common.Option) (common.Artifact, error) {
	// common.BuildClosure is a type alias of *proto.BuildClosure
	f.got = def
	return f.art, f.err
}

func TestServer_Build(t *testing.T) {
	// Shared request body
	reqMsg := &proto.BuildRequest{Closure: &proto.BuildClosure{Root: &proto.Definition{TypeName: "example/type"}}}
	reqBytes, err := gproto.Marshal(reqMsg)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	makeServer := func() (httpstd.Handler, *fakeDB) {
		receipt := &proto.BuildReceipt{Outputs: map[string]*proto.Hash{"out.txt": {Value: "abc123"}}}
		fdb := &fakeDB{art: &fakeArtifact{receipt: receipt}}
		return New(fdb), fdb
	}

	t.Run("default JSON", func(t *testing.T) {
		srv, fdb := makeServer()
		rr := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/build", bytes.NewReader(reqBytes))
		r.Header.Set("Content-Type", "application/protobuf")
		srv.ServeHTTP(rr, r)

		if rr.Code != httpstd.StatusOK {
			t.Fatalf("status: got %d, want %d", rr.Code, httpstd.StatusOK)
		}
		if ct := rr.Result().Header.Get("Content-Type"); ct != "application/json" {
			t.Fatalf("content-type: got %q, want application/json", ct)
		}
		if fdb.got == nil || fdb.got.Root == nil || fdb.got.Root.TypeName != "example/type" {
			t.Fatalf("db.Build not called with expected closure: %+v", fdb.got)
		}

		var got proto.BuildReceipt
		if err := protojson.Unmarshal(rr.Body.Bytes(), &got); err != nil {
			t.Fatalf("unmarshal json: %v", err)
		}
		if got.Outputs["out.txt"].GetValue() != "abc123" {
			t.Fatalf("unexpected receipt: %+v", &got)
		}
	})

	t.Run("protobuf accept", func(t *testing.T) {
		srv, _ := makeServer()
		rr := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/build", bytes.NewReader(reqBytes))
		r.Header.Set("Content-Type", "application/protobuf")
		r.Header.Set("Accept", "application/protobuf")
		srv.ServeHTTP(rr, r)

		if rr.Code != httpstd.StatusOK {
			t.Fatalf("status: got %d, want %d", rr.Code, httpstd.StatusOK)
		}
		if ct := rr.Result().Header.Get("Content-Type"); ct != "application/protobuf" {
			t.Fatalf("content-type: got %q, want application/protobuf", ct)
		}

		var got proto.BuildReceipt
		if err := gproto.Unmarshal(rr.Body.Bytes(), &got); err != nil {
			t.Fatalf("unmarshal proto: %v", err)
		}
		if got.Outputs["out.txt"].GetValue() != "abc123" {
			t.Fatalf("unexpected receipt: %+v", &got)
		}
	})
}

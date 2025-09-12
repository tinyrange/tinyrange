package http

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/tinyrange/tinyrange/build/internal/common"
	"github.com/tinyrange/tinyrange/build/proto"
	"google.golang.org/protobuf/encoding/protojson"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/types/descriptorpb"
)

var protoOpts = gproto.UnmarshalOptions{}

func getDescriptorProto(m gproto.Message) *descriptorpb.FileDescriptorProto {
	md := m.ProtoReflect().Descriptor()

	// Convert the FileDescriptor to a FileDescriptorProto
	return protodesc.ToFileDescriptorProto(md.ParentFile())
}

func unmarshalWithContentType(body io.ReadCloser, contentType string, msg gproto.Message) error {
	switch contentType {
	case "application/protobuf":
		// the input is a proto.BuildRequest message
		content, err := io.ReadAll(body)
		if err != nil {
			return fmt.Errorf("read body: %v", err)
		}
		defer body.Close()

		if err := protoOpts.Unmarshal(content, msg); err != nil {
			return fmt.Errorf("unmarshal proto: %v", err)
		}

		return nil
	case "application/json":
		// the input is a JSON-encoded proto.BuildRequest message
		content, err := io.ReadAll(body)
		if err != nil {
			return fmt.Errorf("read body: %v", err)
		}
		defer body.Close()

		if err := protojson.Unmarshal(content, msg); err != nil {
			return fmt.Errorf("unmarshal json: %v", err)
		}

		return nil
	default:
		return fmt.Errorf("unsupported Content-Type: %s", contentType)
	}
}

func marshalWithAcceptHeader(msg gproto.Message, accept string) ([]byte, string, error) {
	switch accept {
	case "application/protobuf":
		data, err := gproto.Marshal(msg)
		if err != nil {
			return nil, "", fmt.Errorf("marshal proto: %v", err)
		}
		return data, "application/protobuf", nil
	case "application/json", "*/*":
		data, err := protojson.Marshal(msg)
		if err != nil {
			return nil, "", fmt.Errorf("marshal json: %v", err)
		}
		return data, "application/json", nil
	default:
		return nil, "", fmt.Errorf("unsupported Accept header: %s", accept)
	}
}

func New(db common.Database, base string) http.Handler {
	mux := http.NewServeMux()

	route := func(s string) string {
		return fmt.Sprintf(s, base)
	}

	mux.HandleFunc(route("POST %s/build"), func(w http.ResponseWriter, r *http.Request) {
		// check the content type to determine how to parse the body, options are JSON or protobuf
		// default to JSON
		contentType := r.Header.Get("Content-Type")
		if contentType == "" {
			contentType = "application/json"
		}

		var req proto.BuildRequest
		req.Reset()

		if err := unmarshalWithContentType(r.Body, contentType, &req); err != nil {
			slog.Error("unmarshal request", "error", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		art, err := db.Build(req.Closure)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		receipt, err := art.Receipt()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// check what accept headers are set, default to JSON
		accept := r.Header.Get("Accept")
		if accept == "" {
			accept = "application/json"
		}

		respBytes, contentType, err := marshalWithAcceptHeader(receipt, accept)
		if err != nil {
			slog.Error("marshal response", "error", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write(respBytes); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	})

	mux.HandleFunc(route("GET %s/status"), func(w http.ResponseWriter, r *http.Request) {
		// takes a url parameters "key" which is a comma separated list of cache keys to check
		keysParam := r.URL.Query().Get("key")
		if keysParam == "" {
			http.Error(w, "missing key parameter", http.StatusBadRequest)
			return
		}

		keys := strings.Split(keysParam, ",")

		var resp proto.BuildStatusResponse
		resp.Reset()
		for _, key := range keys {
			key = strings.TrimSpace(key)
			if key == "" {
				continue
			}

			status, err := db.GetBuildStatus(key)
			if err != nil {
				http.Error(w, fmt.Sprintf("get build status for key %s: %v", key, err), http.StatusInternalServerError)
				return
			}

			resp.Statuses[key] = status
		}

		accept := r.Header.Get("Accept")
		if accept == "" {
			accept = "application/json"
		}

		respBytes, contentType, err := marshalWithAcceptHeader(&resp, accept)
		if err != nil {
			slog.Error("marshal response", "error", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write(respBytes); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	})

	mux.HandleFunc(route("GET %s/builders"), func(w http.ResponseWriter, r *http.Request) {
		builders, err := db.GetBuilders()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		accept := r.Header.Get("Accept")
		if accept == "" {
			accept = "application/json"
		}

		var ret proto.BuilderList
		ret.Reset()
		for _, b := range builders {
			var meta proto.BuilderMetadata
			meta.Reset()

			meta.TypeName = b.TypeName

			meta.TopLevelType = string(b.Definition.ProtoReflect().Descriptor().Name())

			meta.Definition = getDescriptorProto(b.Definition)

			ret.Builders = append(ret.Builders, &meta)
		}

		respBytes, contentType, err := marshalWithAcceptHeader(&ret, accept)
		if err != nil {
			slog.Error("marshal response", "error", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write(respBytes); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	})

	return mux
}

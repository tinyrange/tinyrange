package http

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/tinyrange/tinyrange/build/internal/common"
	"github.com/tinyrange/tinyrange/build/proto"
	"google.golang.org/protobuf/encoding/protojson"
	gproto "google.golang.org/protobuf/proto"
)

var protoOpts = gproto.UnmarshalOptions{}

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

func marshalWithAcceptHeader(msg gproto.Message, accept string) ([]byte, error) {
	switch accept {
	case "application/protobuf":
		data, err := gproto.Marshal(msg)
		if err != nil {
			return nil, fmt.Errorf("marshal proto: %v", err)
		}
		return data, nil
	case "application/json":
		data, err := protojson.Marshal(msg)
		if err != nil {
			return nil, fmt.Errorf("marshal json: %v", err)
		}
		return data, nil
	default:
		return nil, fmt.Errorf("unsupported Accept header: %s", accept)
	}
}

func New(db common.Database) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /build", func(w http.ResponseWriter, r *http.Request) {
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

		respBytes, err := marshalWithAcceptHeader(receipt, accept)
		if err != nil {
			slog.Error("marshal response", "error", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", accept)
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write(respBytes); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	})

	return mux
}

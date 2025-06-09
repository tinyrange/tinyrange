package build2

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/tinyrange/tinyrange/pkg/hash"
	"github.com/tinyrange/tinyrange/pkg/log"
)

func RegisterBuildDirectoryHandler(
	mux *http.ServeMux,
	buildDir BuildCacheFilesystem,
	// options
	log log.Handler,
	prefix string,
	allowSingleHashes bool,
) {
	mux.Handle(prefix+"/{hash2}/{hash}/"+receiptFileName, http.StripPrefix(prefix, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hash2 := r.PathValue("hash2")
		hashRest := r.PathValue("hash")
		if hash2 == "" || hashRest == "" {
			log.Error("build2 http error: missing hash2 or hash", "hash2", hash2, "hash", hashRest)
			http.Error(w, "missing hash2 or hash", http.StatusBadRequest)
			return
		}

		dir, err := buildDir.GetBuildDirectory(hash.Hash(hash2 + hashRest))
		if err != nil {
			log.Error("build2 http error: failed to get build directory", "hash2", hash2, "hash", hashRest, "error", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		recept, err := dir.ReadReceipt()
		if err != nil {
			log.Error("build2 http error: failed to read receipt", "hash2", hash2, "hash", hashRest, "error", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if _, err := w.Write(recept); err != nil {
			log.Error("build2 http error: failed to write receipt", "hash2", hash2, "hash", hashRest, "error", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	})))

	if allowSingleHashes {
		mux.Handle(prefix+"/{hash}/"+receiptFileName, http.StripPrefix(prefix, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hashValue := r.PathValue("hash")
			if hashValue == "" {
				log.Error("build2 http error: missing hash", "hash", hashValue)
				http.Error(w, "missing hash", http.StatusBadRequest)
				return
			}

			dir, err := buildDir.GetBuildDirectory(hash.Hash(hashValue))
			if err != nil {
				log.Error("build2 http error: failed to get build directory", "hash", hashValue, "error", err)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			recept, err := dir.ReadReceipt()
			if err != nil {
				log.Error("build2 http error: failed to read receipt", "hash", hashValue, "error", err)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			if _, err := w.Write(recept); err != nil {
				log.Error("build2 http error: failed to write receipt", "hash", hashValue, "error", err)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		})))
	}

	mux.Handle(prefix+"/{hash2}/{hash}/"+definitionFileName, http.StripPrefix(prefix, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hash2 := r.PathValue("hash2")
		hashRest := r.PathValue("hash")
		if hash2 == "" || hashRest == "" {
			log.Error("build2 http error: missing hash2 or hash", "hash2", hash2, "hash", hashRest)
			http.Error(w, "missing hash2 or hash", http.StatusBadRequest)
			return
		}

		dir, err := buildDir.GetBuildDirectory(hash.Hash(hash2 + hashRest))
		if err != nil {
			log.Error("build2 http error: failed to get build directory", "hash2", hash2, "hash", hashRest, "error", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		def, err := dir.ReadDefinition()
		if err != nil {
			log.Error("build2 http error: failed to read definition", "hash2", hash2, "hash", hashRest, "error", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if _, err := w.Write(def); err != nil {
			log.Error("build2 http error: failed to write definition", "hash2", hash2, "hash", hashRest, "error", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	})))

	if allowSingleHashes {
		mux.Handle(prefix+"/{hash}/"+definitionFileName, http.StripPrefix(prefix, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hashValue := r.PathValue("hash")
			if hashValue == "" {
				log.Error("build2 http error: missing hash", "hash", hashValue)
				http.Error(w, "missing hash", http.StatusBadRequest)
				return
			}

			dir, err := buildDir.GetBuildDirectory(hash.Hash(hashValue))
			if err != nil {
				log.Error("build2 http error: failed to get build directory", "hash", hashValue, "error", err)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			def, err := dir.ReadDefinition()
			if err != nil {
				log.Error("build2 http error: failed to read definition", "hash", hashValue, "error", err)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			if _, err := w.Write(def); err != nil {
				log.Error("build2 http error: failed to write definition", "hash", hashValue, "error", err)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		})))
	}

	mux.Handle(prefix+"/{hash2}/{hash}/{output}", http.StripPrefix(prefix, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hash2 := r.PathValue("hash2")
		hashRest := r.PathValue("hash")
		output := r.PathValue("output")
		if hash2 == "" || hashRest == "" || output == "" {
			log.Error("build2 http error: missing hash2 or hash or output", "hash2", hash2, "hash", hashRest, "output", output)
			http.Error(w, "missing hash2 or hash or output", http.StatusBadRequest)
			return
		}

		if !strings.HasPrefix(output, outputPrefix) {
			log.Error("build2 http error: invalid output prefix", "output", output)
			http.Error(w, "invalid output prefix", http.StatusBadRequest)
			return
		}
		output = strings.TrimPrefix(output, outputPrefix)

		dir, err := buildDir.GetBuildDirectory(hash.Hash(hash2 + hashRest))
		if err != nil {
			log.Error("build2 http error: failed to get build directory", "hash2", hash2, "hash", hashRest, "error", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		file, err := dir.File(output)
		if err != nil {
			log.Error("build2 http error: failed to get file", "hash2", hash2, "hash", hashRest, "output", output, "error", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		stat, err := file.Stat()
		if err != nil {
			log.Error("build2 http error: failed to stat file", "hash2", hash2, "hash", hashRest, "output", output, "error", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		fh, err := file.Open()
		if err != nil {
			log.Error("build2 http error: failed to open file", "hash2", hash2, "hash", hashRest, "output", output, "error", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer fh.Close()

		http.ServeContent(w, r, r.URL.Path, stat.ModTime(), io.NewSectionReader(fh, 0, stat.Size()))
	})))

	if allowSingleHashes {
		mux.Handle(prefix+"/{hash}/{output}", http.StripPrefix(prefix, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hashValue := r.PathValue("hash")
			output := r.PathValue("output")
			if hashValue == "" || output == "" {
				log.Error("build2 http error: missing hash or output", "hash", hashValue, "output", output)
				http.Error(w, "missing hash or output", http.StatusBadRequest)
				return
			}

			if !strings.HasPrefix(output, outputPrefix) {
				log.Error("build2 http error: invalid output prefix", "output", output)
				http.Error(w, "invalid output prefix", http.StatusBadRequest)
				return
			}
			output = strings.TrimPrefix(output, outputPrefix)

			dir, err := buildDir.GetBuildDirectory(hash.Hash(hashValue))
			if err != nil {
				log.Error("build2 http error: failed to get build directory", "hash", hashValue, "error", err)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			file, err := dir.File(output)
			if err != nil {
				log.Error("build2 http error: failed to get file", "hash", hashValue, "output", output, "error", err)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			stat, err := file.Stat()
			if err != nil {
				log.Error("build2 http error: failed to stat file", "hash", hashValue, "output", output, "error", err)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			fh, err := file.Open()
			if err != nil {
				log.Error("build2 http error: failed to open file", "hash", hashValue, "output", output, "error", err)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			defer fh.Close()

			http.ServeContent(w, r, r.URL.Path, stat.ModTime(), io.NewSectionReader(fh, 0, stat.Size()))
		})))
	}

	mux.Handle(prefix+"/"+MARKER_FILENAME, http.StripPrefix(prefix, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewEncoder(w).Encode(&MarkerHeader{
			Version: MARKER_VERSION,
		}); err != nil {
			log.Error("build2 http error: failed to encode marker header", "error", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	})))

	mux.Handle(prefix+"/", http.StripPrefix(prefix, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Error("build2 http error: not found", "path", r.URL.Path)
		http.NotFound(w, r)
	})))
}

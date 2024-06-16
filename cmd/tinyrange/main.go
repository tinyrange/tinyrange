package main

import (
	"archive/tar"
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	goFs "io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/filesystem/ext4"
	"github.com/tinyrange/tinyrange/pkg/netstack"
	virtualMachine "github.com/tinyrange/tinyrange/pkg/vm"
	gonbd "github.com/tinyrange/tinyrange/third_party/go-nbd"
	"github.com/tinyrange/vm"
)

//go:embed init.star
var _INIT_SCRIPT []byte

type vmBackend struct {
	vm *vm.VirtualMemory
}

// Close implements common.Backend.
func (vm *vmBackend) Close() error {
	return nil
}

// PreferredBlockSize implements common.Backend.
func (*vmBackend) PreferredBlockSize() int64 { return 4096 }

// ReadAt implements common.Backend.
func (vm *vmBackend) ReadAt(p []byte, off int64) (n int, err error) {
	n, err = vm.vm.ReadAt(p, off)
	if err != nil {
		slog.Info("vmBackend readAt", "len", len(p), "off", off, "err", err)
		return 0, nil
	}

	return
}

// WriteAt implements common.Backend.
func (vm *vmBackend) WriteAt(p []byte, off int64) (n int, err error) {
	n, err = vm.vm.WriteAt(p, off)
	if err != nil {
		slog.Info("vmBackend writeAt", "len", len(p), "off", off, "err", err)
		return 0, nil
	}

	return
}

// Size implements common.Backend.
func (vm *vmBackend) Size() (int64, error) {
	return vm.vm.Size(), nil
}

// Sync implements common.Backend.
func (*vmBackend) Sync() error {
	return nil
}

const (
	DEFAULT_REGISTRY = "https://registry-1.docker.io/v2"
)

type imagePlatform struct {
	Architecture string `json:"architecture"`
	Os           string `json:"os"`
	Variant      string `json:"variant"`
}

type ImageManifestIdentifier struct {
	MediaType string        `json:"mediaType"`
	Size      uint64        `json:"size"`
	Digest    string        `json:"digest"`
	Platform  imagePlatform `json:"platform"`
}

type ImageIndexV2 struct {
	SchemaVersion int                       `json:"schemaVersion"`
	MediaType     string                    `json:"mediaType"`
	Manifests     []ImageManifestIdentifier `json:"manifests"`
}

type tokenResponse struct {
	Token       string `json:"token"`
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	IssuedAt    string `json:"issued_at"`
}

type imageConfigIdentifier struct {
	MediaType string `json:"mediaType"`
	Size      uint64 `json:"size"`
	Digest    string `json:"digest"`
}

type ImageLayerIdentifier struct {
	MediaType string `json:"mediaType"`
	Size      uint64 `json:"size"`
	Digest    string `json:"digest"`
}

type ImageManifest struct {
	SchemaVersion int                    `json:"schemaVersion"`
	MediaType     string                 `json:"mediaType"`
	Config        imageConfigIdentifier  `json:"config"`
	Layers        []ImageLayerIdentifier `json:"layers"`
}

func parseAuthenticate(value string) (map[string]string, error) {
	if value == "" {
		return nil, fmt.Errorf("failed to find authenticate header, can't automatically authenticate")
	}

	value = strings.TrimPrefix(value, "Bearer ")

	tokens := strings.Split(value, ",")

	ret := make(map[string]string)

	for _, token := range tokens {
		key, value, ok := strings.Cut(token, "=")
		if !ok {
			return nil, fmt.Errorf("malformed header value")
		}
		value = strings.Trim(value, "\"")
		ret[key] = value
	}

	return ret, nil
}

type ociImageDownloader struct {
	token string
}

func (dl *ociImageDownloader) makeRegistryRequest(method string, url string, acceptHeaders []string) (*http.Response, error) {
	slog.Info("making registry request", "method", method, "url", url, "accept", acceptHeaders)

	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, err
	}

	if dl.token != "" {
		req.Header.Set("Authorization", "Bearer "+dl.token)
	}

	for _, val := range acceptHeaders {
		req.Header.Add("Accept", val)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == http.StatusOK {
		return resp, nil
	} else if resp.StatusCode == http.StatusUnauthorized {
		// Check for a header that describes the authorization needed so we can get a new token.
		authenticate, err := parseAuthenticate(resp.Header.Get("www-authenticate"))
		if err != nil {
			return nil, err
		}

		tokenUrl := fmt.Sprintf("%s?service=%s&scope=%s",
			authenticate["realm"],
			authenticate["service"],
			authenticate["scope"])

		slog.Info("registry auth", "url", tokenUrl)

		resp, err := http.Get(tokenUrl)
		if err != nil {
			return nil, err
		}

		var respJson tokenResponse
		decoder := json.NewDecoder(resp.Body)
		err = decoder.Decode(&respJson)
		if err != nil {
			return nil, err
		}

		dl.token = respJson.Token

		// Remake the request with the new token.
		return dl.makeRegistryRequest(method, url, acceptHeaders)
	} else {
		return nil, fmt.Errorf("failed to handle response code: %s", resp.Status)
	}
}

func (dl *ociImageDownloader) downloadJson(method string, url string, acceptHeaders []string, out any) error {
	resp, err := dl.makeRegistryRequest(method, url, acceptHeaders)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	dec := json.NewDecoder(resp.Body)

	if err := dec.Decode(out); err != nil {
		return err
	}

	return nil
}

func (dl *ociImageDownloader) extractOciImage(fs *ext4.Ext4Filesystem, name string) error {
	imageName, ref, _ := strings.Cut(name, ":")

	if imageName == "library/scratch" {
		return nil
	}

	// download image index.
	indexUrl := fmt.Sprintf("%s/%s/manifests/%s", DEFAULT_REGISTRY, imageName, ref)
	var index ImageIndexV2
	if err := dl.downloadJson("GET", indexUrl, []string{
		"application/vnd.docker.distribution.manifest.list.v2+json",
		"application/vnd.oci.image.index.v1+json",
	}, &index); err != nil {
		return err
	}

	if index.SchemaVersion == 2 {
		var manifestId ImageManifestIdentifier
		for _, manifest := range index.Manifests {
			if manifest.Platform.Architecture == "amd64" {
				manifestId = manifest
			}
		}

		manifestUrl := fmt.Sprintf("%s/%s/manifests/%s", DEFAULT_REGISTRY, imageName, manifestId.Digest)
		var manifest ImageManifest
		if err := dl.downloadJson("GET", manifestUrl, []string{
			"application/vnd.oci.image.manifest.v1+json",
		}, &manifest); err != nil {
			return err
		}

		layers := manifest.Layers
		slices.Reverse(layers)

		for _, layer := range layers {
			layerUrl := fmt.Sprintf("%s/%s/blobs/%s", DEFAULT_REGISTRY, imageName, layer.Digest)
			resp, err := dl.makeRegistryRequest("GET", layerUrl, []string{})
			if err != nil {
				return err
			}

			// assume tar.gz
			if err := filesystem.ExtractReaderTo(resp.Body, ".tar.gz", fs, func(hdr *tar.Header) bool {
				if !strings.HasPrefix(hdr.Name, "/") {
					hdr.Name = "/" + hdr.Name
				}
				if hdr.Typeflag == tar.TypeLink && !strings.HasPrefix(hdr.Linkname, "/") {
					hdr.Linkname = "/" + hdr.Linkname
				}
				slog.Info("", "hdr", hdr)

				return true
			}); err != nil {
				return err
			}
		}
	} else {
		return fmt.Errorf("expected schema version 2 got: %+v", index)
	}

	// return fmt.Errorf("not implemented")
	return nil
}

var (
	storageSize = flag.Int("storage-size", 64, "the size of the VM storage in megabytes")
	image       = flag.String("image", "library/alpine:latest", "the OCI image to boot inside the virtual machine")
)

func tinyRangeMain() error {
	flag.Parse()

	fsSize := int64(*storageSize * 1024 * 1024)

	vmem := vm.NewVirtualMemory(fsSize, 4096)

	fs, err := ext4.CreateExt4Filesystem(vmem, 0, fsSize)
	if err != nil {
		return err
	}

	initExe, err := os.Open("build/init_x86_64")
	if err != nil {
		return err
	}
	defer initExe.Close()

	initRegion, err := vm.NewFileRegion(initExe)
	if err != nil {
		return err
	}

	if err := fs.CreateFile("/init", initRegion); err != nil {
		return err
	}
	if err := fs.Chmod("/init", goFs.FileMode(0755)); err != nil {
		return err
	}

	if err := fs.CreateFile("/init.star", vm.RawRegion(_INIT_SCRIPT)); err != nil {
		return err
	}
	if err := fs.Chmod("/init.star", goFs.FileMode(0755)); err != nil {
		return err
	}

	ociDl := &ociImageDownloader{}

	if err := ociDl.extractOciImage(fs, *image); err != nil {
		return err
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("failed to listen: %v", err)
	}

	backend := &vmBackend{vm: vmem}

	go func() {
		for {
			conn, err := listener.Accept()
			if errors.Is(err, net.ErrClosed) {
				return
			} else if err != nil {
				slog.Error("nbd server failed to accept", "error", err)
				return
			}

			go func(conn net.Conn) {
				slog.Debug("got nbd connection", "remote", conn.RemoteAddr().String())
				err = gonbd.Handle(conn, []gonbd.Export{{
					Name:        "",
					Description: "",
					Backend:     backend,
				}}, &gonbd.Options{
					ReadOnly:           false,
					MinimumBlockSize:   1024,
					PreferredBlockSize: uint32(backend.PreferredBlockSize()),
					MaximumBlockSize:   32*1024*1024 - 1,
				})
				if err != nil {
					slog.Warn("nbd server failed to handle", "error", err)
				}
			}(conn)
		}
	}()

	ns := netstack.New()

	go func() {
		// TODO(joshua): Fix this horrible hack.
		time.Sleep(100 * time.Millisecond)

		listen, err := ns.ListenInternal("tcp", ":80")
		if err != nil {
			slog.Error("failed to listen", "err", err)
			return
		}

		mux := http.NewServeMux()

		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("Hello, World\n"))
		})

		slog.Error("failed to serve", "err", http.Serve(listen, mux))
	}()

	factory, err := virtualMachine.LoadVirtualMachineFactory("hv/qemu/qemu.star")
	if err != nil {
		return err
	}

	virtualMachine, err := factory.Create(
		"local/vmlinux_x86_64",
		"",
		"nbd://"+listener.Addr().String(),
		ns,
	)
	if err != nil {
		return err
	}

	if err := virtualMachine.Run(); err != nil {
		return err
	}

	return nil
}

func main() {
	if err := tinyRangeMain(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

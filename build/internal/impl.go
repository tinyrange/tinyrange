package internal

import (
	"bytes"
	"fmt"
	"io"
	"net/http"

	"github.com/schollz/progressbar/v3"
	"github.com/tinyrange/tinyrange/archive"
	"github.com/tinyrange/tinyrange/build/hash"
	_ "github.com/tinyrange/tinyrange/build/internal/builder"
	"github.com/tinyrange/tinyrange/build/internal/common"
	"github.com/tinyrange/tinyrange/build/internal/registry"
	"github.com/tinyrange/tinyrange/build/proto"
	protob "google.golang.org/protobuf/proto"
)

type nopCloser struct {
	io.Writer
}

func (n *nopCloser) Close() error { return nil }

type wrapCloser struct {
	io.Reader
	io.Closer
}

type archiveWriter struct {
	*archive.Writer
	index    io.Closer
	contents io.Closer
}

func (a *archiveWriter) Close() error {
	if err := a.index.Close(); err != nil {
		a.contents.Close()
		return err
	}
	if err := a.contents.Close(); err != nil {
		return err
	}
	return nil
}

type contextImpl struct {
	db      *databaseImpl
	hash    hash.Hash
	msg     *proto.Definition
	depends []*proto.BuildClosure

	// temp
	files map[string]*bytes.Buffer
}

// ProgressBar implements common.Context.
func (c *contextImpl) ProgressBar(name string, size int64, r io.ReadCloser) io.ReadCloser {
	pb := progressbar.DefaultBytes(size, name)
	return &wrapCloser{Reader: io.TeeReader(r, pb), Closer: r}
}

// Create implements common.Context.
func (c *contextImpl) Create(ft common.FileType) (common.WritableFile, error) {
	if _, ok := c.files[string(ft)]; ok {
		return nil, fmt.Errorf("file of type %s already created", ft)
	}
	buf := &bytes.Buffer{}
	c.files[string(ft)] = buf
	return &nopCloser{buf}, nil
}

func (c *contextImpl) CreateArchive() (common.ArchiveWriter, error) {
	index, err := c.Create(common.FileType_ArchiveIndex)
	if err != nil {
		return nil, err
	}
	contents, err := c.Create(common.FileType_ArchiveContents)
	if err != nil {
		index.Close()
		return nil, err
	}

	w, err := archive.NewWriter(index, contents)
	if err != nil {
		contents.Close()
		return nil, err
	}

	return &archiveWriter{w, index, contents}, nil
}

// HttpClient implements common.Context.
func (c *contextImpl) HttpClient() *http.Client {
	return http.DefaultClient
}

// Decode implements common.Context.
func (c *contextImpl) Decode(msg protob.Message) error {
	return c.msg.Payload.UnmarshalTo(msg)
}

// Hash implements common.Context.
func (c *contextImpl) Hash() hash.Hash { return c.hash }

var (
	_ common.Context = &contextImpl{}
)

type artifactImpl struct {
}

type databaseImpl struct {
	factory *factoryImpl
}

// Factory implements common.Database.
func (d *databaseImpl) Factory() common.Factory {
	return d.factory
}

// Build implements build.Database.
func (d *databaseImpl) Build(closure *proto.BuildClosure, opt ...common.Option) (common.Artifact, error) {
	// get the root definition
	def := closure.Root

	// resolve the builder
	builder := registry.Get(def.TypeName)
	if builder == nil {
		return nil, fmt.Errorf("unknown builder: %s", def.TypeName)
	}

	ctx := &contextImpl{
		db:      d,
		hash:    hash.Hash(""),
		msg:     def,
		depends: closure.Dependencies,
		files:   map[string]*bytes.Buffer{},
	}

	if err := builder.Build(ctx); err != nil {
		return nil, err
	}

	return &artifactImpl{}, nil
}

var (
	_ common.Database = &databaseImpl{}
)

func New() (common.Database, error) {
	return &databaseImpl{
		factory: &factoryImpl{},
	}, nil
}

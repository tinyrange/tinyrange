package internal

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"time"

	"github.com/schollz/progressbar/v3"
	"github.com/tinyrange/tinyrange/archive"
	_ "github.com/tinyrange/tinyrange/build/internal/builder"
	"github.com/tinyrange/tinyrange/build/internal/common"
	"github.com/tinyrange/tinyrange/build/internal/registry"
	"github.com/tinyrange/tinyrange/build/proto"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

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
	write   common.WritableBuildCacheDirectory
	receipt *proto.BuildReceipt
	db      *databaseImpl
	hash    *proto.Hash
	msg     *proto.Definition
	depends map[string]*proto.Definition
	writers map[common.FileType]common.WritableFile
}

// ProgressBar implements common.Context.
func (c *contextImpl) ProgressBar(name string, size int64, r io.ReadCloser) io.ReadCloser {
	pb := progressbar.DefaultBytes(size, name)
	return &wrapCloser{Reader: io.TeeReader(r, pb), Closer: r}
}

// Create implements common.Context.
func (c *contextImpl) Create(ft common.FileType) (common.WritableFile, error) {
	// check if we already have a writer for this type
	if _, ok := c.writers[ft]; ok {
		return nil, fmt.Errorf("file type %q already created", ft)
	}

	w, err := c.write.CreateFile(ft)
	if err != nil {
		return nil, err
	}
	c.writers[ft] = w
	return w, nil
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
func (c *contextImpl) Decode(msg gproto.Message) error {
	return c.msg.Payload.UnmarshalTo(msg)
}

// Hash implements common.Context.
func (c *contextImpl) Hash() *proto.Hash { return c.hash }

var (
	_ common.Context = &contextImpl{}
)

type artifactImpl struct {
	cacheDir common.BuildCacheDirectory
}

// Definition implements common.Artifact.
func (a *artifactImpl) Definition() (*proto.Definition, error) {
	def, err := a.cacheDir.ReadDefinition()
	if err != nil {
		return nil, err
	}

	var msg proto.Definition
	msg.Reset()

	if err := gproto.Unmarshal(def, &msg); err != nil {
		return nil, err
	}

	return &msg, nil
}

// Receipt implements common.Artifact.
func (a *artifactImpl) Receipt() (*proto.BuildReceipt, error) {
	receipt, err := a.cacheDir.ReadReceipt()
	if err != nil {
		return nil, err
	}

	var msg proto.BuildReceipt
	msg.Reset()

	if err := gproto.Unmarshal(receipt, &msg); err != nil {
		return nil, err
	}

	return &msg, nil
}

// Open implements common.Artifact.
func (a *artifactImpl) Open(ft common.FileType) (common.File, error) {
	return a.cacheDir.OpenFile(ft)
}

type databaseImpl struct {
	factory *factoryImpl
	cache   common.BuildCache
}

// Factory implements common.Database.
func (d *databaseImpl) Factory() common.Factory {
	return d.factory
}

func (d *databaseImpl) lockAndBuild(closure *proto.BuildClosure, opt ...common.Option) (common.Artifact, error) {
	write, err := d.cache.OpenWrite(closure.Root.Hash())
	if err != nil {
		return nil, err
	}

	// get the root definition
	def := closure.Root

	if err := write.WriteDefinition(def.AsBytes()); err != nil {
		return nil, err
	}

	// resolve the builder
	builder := registry.Get(def.TypeName)
	if builder == nil {
		return nil, fmt.Errorf("unknown builder: %s", def.TypeName)
	}

	ctx := &contextImpl{
		receipt: &proto.BuildReceipt{
			StartTime: timestamppb.New(time.Now()),
			Outputs:   map[string]*proto.Hash{},
		},
		write:   write,
		db:      d,
		hash:    def.Hash(),
		msg:     def,
		depends: map[string]*proto.Definition{},
		writers: map[common.FileType]common.WritableFile{},
	}

	for _, dep := range closure.Dependencies {
		ctx.depends[string(dep.Hash().Value)] = dep
	}

	if err := builder.Build(ctx); err != nil {
		return nil, err
	}

	ctx.receipt.EndTime = timestamppb.New(time.Now())

	for ft, w := range ctx.writers {
		ctx.receipt.Outputs[string(ft)] = w.Hash()
	}

	if err := write.WriteReceipt(ctx.receipt.AsBytes()); err != nil {
		return nil, err
	}

	return &artifactImpl{
		cacheDir: write,
	}, nil
}

// Build implements build.Database.
func (d *databaseImpl) Build(closure *proto.BuildClosure, opt ...common.Option) (common.Artifact, error) {
	rootHash := closure.Root.Hash()

	read, err := d.cache.OpenRead(rootHash)
	if errors.Is(err, fs.ErrNotExist) {
		return d.lockAndBuild(closure, opt...)
	} else if err != nil {
		return nil, err
	}

	return &artifactImpl{cacheDir: read}, nil
}

var (
	_ common.Database = &databaseImpl{}
)

func New(cache common.BuildCache) (common.Database, error) {
	return &databaseImpl{
		factory: &factoryImpl{},
		cache:   cache,
	}, nil
}

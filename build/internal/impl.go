package internal

import (
	"fmt"

	"github.com/tinyrange/tinyrange/build/hash"
	_ "github.com/tinyrange/tinyrange/build/internal/builder"
	"github.com/tinyrange/tinyrange/build/internal/common"
	"github.com/tinyrange/tinyrange/build/internal/registry"
	"github.com/tinyrange/tinyrange/build/proto"
	protob "google.golang.org/protobuf/proto"
)

type contextImpl struct {
	db   *databaseImpl
	hash hash.Hash
	msg  *proto.Definition
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
func (d *databaseImpl) Build(def *proto.Definition, opt ...common.Option) (common.Artifact, error) {
	// resolve the builder
	builder := registry.Get(def.TypeName)
	if builder == nil {
		return nil, fmt.Errorf("unknown builder: %s", def.TypeName)
	}

	ctx := &contextImpl{
		db:   d,
		hash: hash.Hash(""),
		msg:  def,
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

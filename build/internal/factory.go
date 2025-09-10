package internal

import (
	"github.com/tinyrange/tinyrange/build/internal/common"
	"github.com/tinyrange/tinyrange/build/proto"
	protob "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

type definitionWalker struct {
	defs map[string]*proto.Definition
}

func (w *definitionWalker) Definitions() map[string]*proto.Definition {
	return w.defs
}

func (w *definitionWalker) walkDefinition(def *proto.Definition) bool {
	hash := def.Hash()
	if _, ok := w.defs[string(hash.Value)]; ok {
		return false
	}
	w.defs[string(hash.Value)] = def
	return true
}

func (w *definitionWalker) walkClosure(def *proto.BuildClosure) {
	if !w.walkDefinition(def.Root) {
		return
	}
	for _, dep := range def.Dependencies {
		w.walkDefinition(dep)
	}
}

type factoryImpl struct {
}

func (f *factoryImpl) pack(typeName string, body protob.Message, depends ...common.BuildClosure) common.BuildClosure {
	closure := &proto.BuildClosure{}

	def := &proto.Definition{
		Payload: &anypb.Any{},
	}
	def.TypeName = typeName
	def.Payload.MarshalFrom(body)

	closure.Root = def
	w := &definitionWalker{
		defs: map[string]*proto.Definition{},
	}
	w.walkClosure(closure)
	for _, def := range w.Definitions() {
		closure.Dependencies = append(closure.Dependencies, def)
	}

	return closure
}

// NewExtractArchive implements common.Factory.
func (f *factoryImpl) NewExtractArchive(src common.BuildClosure, archiveType proto.ArchiveType, compressionType proto.CompressionType) common.BuildClosure {
	var source proto.ArchiveSource
	var children []common.BuildClosure
	switch src.Root.TypeName {
	case common.TYPE_NAME_FETCH_HTTP:
		var http proto.FetchHttpDefinition
		src.Root.Payload.UnmarshalTo(&http)
		source = &proto.FileSource_FetchHttp{
			FetchHttp: &http,
		}
	default:
		hash := src.Root.Hash()

		source = &proto.FileSource_Reference{
			Reference: &proto.DefinitionReference{
				Hash: string(hash.Value),
			},
		}
		children = append(children, src)
	}

	return f.pack(common.TYPE_NAME_EXTRACT_ARCHIVE, &proto.ExtractArchiveDefinition{
		Source:          &proto.FileSource{Source: source},
		ArchiveType:     archiveType,
		CompressionType: compressionType,
	}, children...)
}

// NewFetchHttp implements common.Factory.
func (f *factoryImpl) NewFetchHttp(url string) common.BuildClosure {
	return f.pack(common.TYPE_NAME_FETCH_HTTP, &proto.FetchHttpDefinition{
		Url: url,
	})
}

// NewWriteFile implements common.Factory.
func (f *factoryImpl) NewWriteFile(content []byte) common.BuildClosure {
	return f.pack(common.TYPE_NAME_WRITE_FILE, &proto.WriteFileDefinition{
		Content: content,
	})
}

var (
	_ common.Factory = &factoryImpl{}
)

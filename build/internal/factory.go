package internal

import (
	"github.com/tinyrange/tinyrange/build/internal/common"
	"github.com/tinyrange/tinyrange/build/proto"
	protob "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

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

	visited := map[proto.Hash]bool{}

	var visit func(def *proto.BuildClosure)
	visit = func(def *proto.BuildClosure) {
		h := def.Root.Hash()
		if visited[h] {
			return
		}
		visited[h] = true
		closure.Dependencies = append(closure.Dependencies, def)
		for _, d := range def.Dependencies {
			visit(d)
		}
	}

	for _, d := range depends {
		visit(d)
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
				Hash: string(hash),
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

var (
	_ common.Factory = &factoryImpl{}
)

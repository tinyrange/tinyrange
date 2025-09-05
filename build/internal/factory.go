package internal

import (
	"github.com/tinyrange/tinyrange/build/internal/common"
	"github.com/tinyrange/tinyrange/build/proto"
	protob "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

type factoryImpl struct {
}

func (f *factoryImpl) pack(typeName string, body protob.Message) common.Definition {
	def := &proto.Definition{
		Payload: &anypb.Any{},
	}
	def.TypeName = typeName
	def.Payload.MarshalFrom(body)
	return def
}

// NewExtractArchive implements common.Factory.
func (f *factoryImpl) NewExtractArchive(src common.Definition, archiveType proto.ArchiveType, compressionType proto.CompressionType) common.Definition {
	var source proto.ArchiveSource
	switch src.TypeName {
	case common.TYPE_NAME_FETCH_HTTP:
		var http proto.FetchHttpDefinition
		src.Payload.UnmarshalTo(&http)
		source = &proto.ExtractArchiveDefinition_FetchHttp{
			FetchHttp: &http,
		}
	default:
		panic("unsupported source type: " + src.TypeName)
	}

	return f.pack(common.TYPE_NAME_EXTRACT_ARCHIVE, &proto.ExtractArchiveDefinition{
		Source:          source,
		ArchiveType:     archiveType,
		CompressionType: compressionType,
	})
}

// NewFetchHttp implements common.Factory.
func (f *factoryImpl) NewFetchHttp(url string) common.Definition {
	return f.pack(common.TYPE_NAME_FETCH_HTTP, &proto.FetchHttpDefinition{
		Url: url,
	})
}

var (
	_ common.Factory = &factoryImpl{}
)

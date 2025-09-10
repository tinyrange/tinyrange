package builder

import (
	"fmt"
	"io"

	"github.com/tinyrange/tinyrange/build/internal/common"
	"github.com/tinyrange/tinyrange/build/proto"
)

func ReaderFromSource(ctx common.Context, source *proto.FileSource) (io.ReadCloser, error) {
	switch src := source.Source.(type) {
	case *proto.FileSource_FetchHttp:
		return ReaderFromFetchHttp(ctx, src.FetchHttp)
	default:
		return nil, fmt.Errorf("unsupported source type: %T", src)
	}
}

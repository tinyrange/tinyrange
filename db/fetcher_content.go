package db

import (
	"fmt"
	"log/slog"

	"github.com/tinyrange/pkg2/memtar"
	"go.starlark.net/starlark"
)

type ContentFetcher struct {
	Func *starlark.Function
	Args starlark.Tuple
}

// Attr implements starlark.HasAttrs.
func (fetch *ContentFetcher) Attr(name string) (starlark.Value, error) {
	return nil, nil
}

// AttrNames implements starlark.HasAttrs.
func (fetch *ContentFetcher) AttrNames() []string {
	return []string{}
}

func (fetch *ContentFetcher) FetchContents(url string) (memtar.TarReader, error) {
	thread := &starlark.Thread{}

	res, err := starlark.Call(thread, fetch.Func,
		append(append(starlark.Tuple{fetch}, fetch.Args...), starlark.String(url)),
		[]starlark.Tuple{},
	)
	if err != nil {
		if sErr, ok := err.(*starlark.EvalError); ok {
			slog.Error("got starlark error", "error", sErr, "backtrace", sErr.Backtrace())
		}
		return nil, fmt.Errorf("error calling user callback: %s", err)
	}

	if reader, ok := res.(memtar.TarReader); ok {
		return reader, nil
	} else {
		return nil, fmt.Errorf("could not convert %s to Archive", res.Type())
	}
}

func (*ContentFetcher) String() string        { return "ContentFetcher" }
func (*ContentFetcher) Type() string          { return "ContentFetcher" }
func (*ContentFetcher) Hash() (uint32, error) { return 0, fmt.Errorf("ContentFetcher is not hashable") }
func (*ContentFetcher) Truth() starlark.Bool  { return starlark.True }
func (*ContentFetcher) Freeze()               {}

var (
	_ starlark.Value    = &ContentFetcher{}
	_ starlark.HasAttrs = &ContentFetcher{}
)

package db

import (
	"bytes"
	"encoding/json"

	"github.com/nix-community/go-nix/pkg/derivation"
	starlarkjson "go.starlark.net/lib/json"
	"go.starlark.net/starlark"
)

func parseNixDerivation(thread *starlark.Thread, contents string) (starlark.Value, error) {
	drv, err := derivation.ReadDerivation(bytes.NewReader([]byte(contents)))
	if err != nil {
		return nil, err
	}

	content, err := json.Marshal(drv)
	if err != nil {
		return nil, err
	}

	return starlark.Call(
		thread,
		starlarkjson.Module.Members["decode"],
		starlark.Tuple{starlark.String(content)},
		[]starlark.Tuple{},
	)
}

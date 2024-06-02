package db

import (
	"github.com/kythe/llvmbzlgen/cmakelib/ast"
	"go.starlark.net/starlark"
)

func parseCMake(f StarFileIf) (starlark.Value, error) {
	fh, err := f.Open()
	if err != nil {
		return starlark.None, err
	}
	defer fh.Close()

	parser := ast.NewParser()

	ast, err := parser.Parse(fh)
	if err != nil {
		return starlark.None, err
	}

	// for _, cmd := range ast.Commands {
	// 	slog.Info("", "cmd", cmd)
	// }

	_ = ast

	return starlark.None, nil
}

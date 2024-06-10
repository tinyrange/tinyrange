package main

import (
	"fmt"
	"log/slog"
	"os"

	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

func initMain() error {
	globals := starlark.StringDict{}

	globals["exit"] = starlark.NewBuiltin("exit", func(
		thread *starlark.Thread,
		fn *starlark.Builtin,
		args starlark.Tuple,
		kwargs []starlark.Tuple,
	) (starlark.Value, error) {
		os.Exit(0)

		return starlark.None, nil
	})

	thread := &starlark.Thread{Name: "init"}

	decls, err := starlark.ExecFileOptions(&syntax.FileOptions{Set: true, While: true, TopLevelControl: true}, thread, "/init.star", nil, globals)
	if err != nil {
		return err
	}

	mainFunc, ok := decls["main"]
	if !ok {
		return fmt.Errorf("expected Callable got %s", mainFunc.Type())
	}

	_, err = starlark.Call(thread, mainFunc, starlark.Tuple{}, []starlark.Tuple{})
	if err != nil {
		return err
	}

	return nil
}

func main() {
	if err := initMain(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

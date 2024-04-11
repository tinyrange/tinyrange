package db

import (
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

func evalStarlark(contents string, kwArgs []starlark.Tuple) (starlark.Value, error) {
	thread := &starlark.Thread{}

	env := starlark.StringDict{}

	for _, arg := range kwArgs {
		k, _ := starlark.AsString(arg[0])
		v := arg[1]

		env[k] = v
	}

	declared, err := starlark.ExecFileOptions(&syntax.FileOptions{
		TopLevelControl: true,
		Recursion:       true,
		Set:             true,
		GlobalReassign:  true,
	}, thread, "<eval>", contents, env)
	if err != nil {
		return starlark.None, err
	}

	ret := starlark.NewDict(len(declared))
	for k, v := range declared {
		ret.SetKey(starlark.String(k), v)
	}

	return ret, err
}

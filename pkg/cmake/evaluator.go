package cmake

import (
	"log/slog"

	"github.com/kythe/llvmbzlgen/cmakelib/ast"
	"go.starlark.net/starlark"
)

type evaluator struct {
	parent  *evaluator
	values  *starlark.Dict
	dirname string
}

func (eval *evaluator) childScope(newScope bool, dirname string) *evaluator {
	if !newScope {
		return &evaluator{
			parent:  eval,
			values:  eval.values,
			dirname: dirname,
		}
	} else {
		return &evaluator{
			parent:  eval,
			values:  starlark.NewDict(32),
			dirname: dirname,
		}
	}
}

func (eval *evaluator) Defined(k string) bool {
	if _, ok, _ := eval.values.Get(starlark.String(k)); ok {
		return true
	}

	if eval.parent != nil {
		return eval.parent.Defined(k)
	}

	return false
}

// Get implements ast.Bindings.
func (eval *evaluator) Get(k string) string {
	if k == "CMAKE_CURRENT_LIST_DIR" {
		return eval.dirname
	} else if k == "CMAKE_CURRENT_SOURCE_DIR" {
		return eval.dirname
	}

	v, ok, err := eval.values.Get(starlark.String(k))
	if err != nil {
		slog.Error("CMakeEvaluator: failed to get", "k", k, "err", err)
	}

	if !ok {
		if eval.parent != nil {
			return eval.parent.Get(k)
		} else {
			return ""
		}
	}

	str, ok := starlark.AsString(v)
	if !ok {
		return v.String()
	} else {
		return str
	}
}

// GetCache implements ast.Bindings.
func (eval *evaluator) GetCache(k string) string {
	return eval.Get(k)
}

// GetEnv implements ast.Bindings.
func (eval *evaluator) GetEnv(k string) string {
	return eval.Get(k)
}

var (
	_ ast.Bindings = &evaluator{}
)

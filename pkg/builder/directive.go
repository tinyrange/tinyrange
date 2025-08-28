package builder

import (
	"fmt"

	"go.starlark.net/starlark"

	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
)

func AsDirective(val starlark.Value) (common.Directive, error) {
	if starDir, ok := val.(*common.StarDirective); ok {
		return starDir.Directive, nil
	} else if directive, ok := val.(common.Directive); ok {
		return directive, nil
	} else if file, ok := val.(filesystem.File); ok {
		def, err := Factory.NewDefinitionFromFile(file)
		if err != nil {
			return nil, err
		}

		if dir, ok := def.(common.Directive); ok {
			return dir, nil
		} else {
			return nil, fmt.Errorf("could not convert %T to Directive", def)
		}
	} else if ark, ok := val.(filesystem.Archive); ok {
		def, err := SourceFromArchive(ark)
		if err != nil {
			return nil, err
		}

		if dir, ok := def.(common.Directive); ok {
			return dir, nil
		} else {
			return nil, fmt.Errorf("could not convert %T to Directive", def)
		}
	} else {
		return nil, fmt.Errorf("could not convert %s to Directive", val.Type())
	}
}

func AsDirectiveList(it starlark.Iterable) ([]common.Directive, error) {
	if it == nil {
		return nil, nil
	}

	var val starlark.Value

	var directives []common.Directive

	directiveIter := it.Iterate()
	defer directiveIter.Done()

	for directiveIter.Next(&val) {
		dir, err := AsDirective(val)
		if err != nil {
			return nil, err
		}

		directives = append(directives, dir)
	}

	return directives, nil
}

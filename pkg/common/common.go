package common

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"

	starlarkjson "go.starlark.net/lib/json"
	"go.starlark.net/starlark"
)

var StarlarkJsonEncode = starlarkjson.Module.Members["encode"].(*starlark.Builtin).CallInternal
var StarlarkJsonDecode = starlarkjson.Module.Members["decode"].(*starlark.Builtin).CallInternal

func ToStringList(it starlark.Iterable) ([]string, error) {
	iter := it.Iterate()
	defer iter.Done()

	var ret []string

	var val starlark.Value
	for iter.Next(&val) {
		str, ok := starlark.AsString(val)
		if !ok {
			return nil, fmt.Errorf("could not convert %s to string", val.Type())
		}

		ret = append(ret, str)
	}

	return ret, nil
}

func GetSha256Hash(content []byte) string {
	sum := sha256.Sum256(content)

	return hex.EncodeToString(sum[:])
}

// From: https://stackoverflow.com/questions/12518876/how-to-check-if-a-file-exists-in-go
func Exists(name string) (bool, error) {
	_, err := os.Stat(name)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

func Ensure(path string, mode os.FileMode) error {
	exists, err := Exists(path)
	if err != nil {
		return fmt.Errorf("failed to check for path: %v", err)
	}

	if !exists {
		err := os.Mkdir(path, mode)
		if err != nil {
			return fmt.Errorf("failed to create directory: %v", err)
		}
	}

	return nil
}

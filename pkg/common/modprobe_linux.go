package common

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"slices"
	"strings"
	"syscall"

	"github.com/tinyrange/tinyrange/pkg/log"
	"github.com/tinyrange/tinyrange/pkg/path"
	"golang.org/x/sys/unix"
)

func parseDeps(filename string) (map[string][]string, error) {
	ret := make(map[string][]string)

	content, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(content), "\n")

	// Each line looks like `moduleName: dep1 dep2 dep3`
	for _, line := range lines {
		line := strings.Trim(line, "\n")

		tokens := strings.Split(line, ": ")
		if len(tokens) == 2 {
			ret[tokens[0]] = strings.Split(tokens[1], " ")
		} else {
			ret[strings.TrimSuffix(line, ":")] = []string{}
		}
	}

	return ret, nil
}

func LoadModule(module string) error {
	moduleFile, err := os.Open(module)
	if err != nil {
		return fmt.Errorf("error opening module: %s", err)
	}
	defer moduleFile.Close()

	var reader io.Reader = moduleFile

	if strings.HasSuffix(module, ".gz") {
		reader, err = gzip.NewReader(reader)
		if err != nil {
			return fmt.Errorf("error decompressing module: %s", err)
		}
	}

	moduleContent, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("error reading module: %s", err)
	}

	log.Default().Debug("loading module", "name", module)
	err = unix.InitModule(moduleContent, "")
	if sErr, ok := err.(syscall.Errno); ok {
		if !errors.Is(sErr, fs.ErrExist) {
			return fmt.Errorf("error loading module: %s", err)
		}
	} else if err != nil {
		return fmt.Errorf("error loading module: %s", err)
	}

	return nil
}

// Probes for a given kernel module and loads it using the dependency information in /lib/modules.
func Modprobe(name string) error {
	// If there's no /lib/modules then don't try to load a module.
	exists, _ := Exists("/lib/modules")
	if !exists {
		return nil
	}

	files, err := os.ReadDir("/lib/modules")
	if err != nil {
		return err
	}

	if len(files) == 0 {
		return fmt.Errorf("len(files) == 0")
	}

	kernelDir := path.Native.Join("/lib/modules", files[0].Name())

	deps, err := parseDeps(path.Native.Join(kernelDir, "modules.dep"))
	if err != nil {
		log.Default().Warn("could not parse dependencies", "error", err)
		return nil
	}

	for k, v := range deps {
		if strings.HasSuffix(k, "/"+name+".ko.gz") {
			modList := make([]string, len(v))

			copy(modList, v)

			slices.Reverse(modList)

			// Load all the dependencies in order.
			for _, dep := range modList {
				err := LoadModule(path.Native.Join(kernelDir, dep))
				if err != nil {
					return err
				}
			}

			// Finally load the requested module.
			err := LoadModule(path.Native.Join(kernelDir, k))
			if err != nil {
				return err
			}

			return nil
		}
	}

	return fmt.Errorf("module with name %s not found", name)
}

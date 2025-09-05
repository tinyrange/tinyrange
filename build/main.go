package build

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func Main() error {
	fs := flag.NewFlagSet(filepath.Base(os.Args[0]), flag.ExitOnError)

	fs.Parse(os.Args[1:])

	return fmt.Errorf("not implemented")
}

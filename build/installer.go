//go:build !windows

package main

import (
	_ "embed"
	"log/slog"
	"os"

	"github.com/tinyrange/tinyrange/pkg/installer"
)

//go:embed pkg2
var PKG2_BINARY []byte

//go:embed tinyrange
var TINYRANGE_BINARY []byte

func main() {
	if err := installer.InstallerMain(PKG2_BINARY, TINYRANGE_BINARY); err != nil {
		slog.Error("Fatal", "err", err)
		os.Exit(1)
	}
}

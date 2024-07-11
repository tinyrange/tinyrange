//go:build windows

package main

import (
	_ "embed"
	"log/slog"
	"os"

	"github.com/tinyrange/tinyrange/pkg/installer"
)

//go:embed pkg2.exe
var PKG2_BINARY []byte

//go:embed tinyrange.exe
var TINYRANGE_BINARY []byte

func main() {
	if err := installer.InstallerMain(TINYRANGE_BINARY, PKG2_BINARY); err != nil {
		slog.Error("Fatal", "err", err)
		os.Exit(1)
	}
}

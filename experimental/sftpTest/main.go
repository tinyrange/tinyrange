package main

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"time"

	"github.com/tinyrange/tinyrange/pkg/builder"
	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/database"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/sftp"
)

func appMain() error {
	if err := common.EnableVerbose(); err != nil {
		return err
	}

	db := database.New(common.GetDefaultBuildDir())

	// Just fetch ubuntu.
	def := builder.NewFetchOCIImageDefinition("", "library/ubuntu", "", "")

	// Get all the fragments/layers from the docker image.
	frags, err := def.AsFragments(db.NewBuildContext(def))
	if err != nil {
		return err
	}

	// Create a directory and extract all archives into it.
	dir := filesystem.NewMemoryDirectory()

	dir2, err := dir.Mkdir("name")
	if err != nil {
		return err
	}

	for _, frag := range frags {
		if frag.Archive != nil {
			ark, err := filesystem.ReadArchiveFromFile(filesystem.NewLocalFile(frag.Archive.HostFilename, def))
			if err != nil {
				return err
			}

			if err := filesystem.ExtractArchive(ark, dir2); err != nil {
				return err
			}
		} else {
			return fmt.Errorf("unimplemented fragment: %+v", frag)
		}
	}

	// Finally create the SFTP server and run it on the directory.
	svr := sftp.NewInternalServer(dir, "127.0.0.1:2223")

	slog.Info("listening", "addr", svr.Addr)

	if err := svr.Run(func(network, addr string) (net.Listener, error) {
		return net.Listen(network, addr)
	}); err != nil {
		return err
	}

	for {
		time.Sleep(1 * time.Hour)
	}
}

func main() {
	if err := appMain(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

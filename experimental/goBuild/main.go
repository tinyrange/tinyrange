package main

import (
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime"
	"strings"

	"github.com/tinyrange/tinyrange/pkg/archive"
	"github.com/tinyrange/tinyrange/pkg/build2"
	"github.com/tinyrange/tinyrange/pkg/builder"
	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/config"
	"github.com/tinyrange/tinyrange/pkg/database"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/path"
	"golang.org/x/mod/modfile"
)

var (
	buildPath    = flag.String("build", "local/goBuild", "path to build output")
	proxy        = flag.String("proxy", "https://proxy.golang.org", "proxy to use for downloading modules")
	tinyrangeRev = flag.String("rev", "39fd3fde13f6ea5d93bc5f6ee5d36782e011efa6", "revision of tinyrange to use")
)

func getArchiveEntriesFromDefinition(db common.PackageDatabase, def common.BuildDefinition) ([]filesystem.Entry, error) {
	result, err := db.Builder().Build(def, common.BuildOptions{})
	if err != nil {
		return nil, err
	}

	resultFile, err := result.Default()
	if err != nil {
		return nil, err
	}

	ark, err := archive.ReadArchiveFromFile(resultFile)
	if err != nil {
		return nil, err
	}

	return ark.Entries()
}

func downloadGoModule(modPath, version string) common.BuildDefinition {
	return builder.Factory.NewFetchHttpBuildDefinition(
		fmt.Sprintf("%s/%s/@v/%s.zip", *proxy, modPath, version),
		0, nil,
	)
}

func downloadGoModuleFile(modPath, version string) common.BuildDefinition {
	return builder.Factory.NewFetchHttpBuildDefinition(
		fmt.Sprintf("%s/%s/@v/%s.mod", *proxy, modPath, version),
		0, nil,
	)
}

func downloadAndExtractGoModule(modPath, version string) builder.ReadArchiveDefinition {
	return builder.Factory.NewReadArchiveBuildDefinition(
		downloadGoModule(modPath, version),
		".zip",
	)
}

func formatPath(path string) string {
	// Make path lowercase but put %21 before any capital letters
	var result strings.Builder

	for _, c := range path {
		if c >= 'A' && c <= 'Z' {
			result.WriteString("!")
		}
		result.WriteRune(rune(strings.ToLower(string(c))[0]))
	}

	return result.String()
}

func appMain() error {
	flag.Parse()

	slog.Info("creating database")

	db, err := database.New(func(db common.PackageDatabase) (common.Builder, error) {
		if err := common.Ensure(*buildPath, os.ModePerm); err != nil {
			return nil, err
		}

		buildDir := filesystem.NewLocalMutableDirectory(*buildPath)
		logger := build2.NewSimpleLogger()
		return build2.New(buildDir, db, 1, logger.Group("root")), nil
	})
	if err != nil {
		return err
	}

	var dirs []common.Directive

	tinyrangeUrl := fmt.Sprintf("https://github.com/tinyrange/tinyrange/archive/%s.tar.gz", *tinyrangeRev)

	tinyrangeDef := builder.Factory.NewReadArchiveBuildDefinition(
		builder.Factory.NewFetchHttpBuildDefinition(tinyrangeUrl, 0, nil),
		".tar.gz",
	)

	dirs = append(dirs, common.DirectiveArchive{Definition: tinyrangeDef, Target: "/src"})

	var modFile *modfile.File

	ents, err := getArchiveEntriesFromDefinition(db, tinyrangeDef)
	if err != nil {
		return err
	}

	var modDir string

	for _, ent := range ents {
		if path.Unix.Base(ent.Name()) == "go.mod" {
			modDir = path.Unix.Dir(ent.Name())

			fh, err := ent.Open()
			if err != nil {
				return err
			}

			contents, err := io.ReadAll(fh)
			if err != nil {
				return err
			}

			modFile, err = modfile.Parse("go.mod", contents, nil)
			if err != nil {
				return err
			}

			break
		}
	}

	if modFile == nil {
		return fmt.Errorf("could not find go.mod in archive")
	}

	toolchain := modFile.Go.Version
	dirs = append(dirs, common.DirectiveArchive{
		Definition: downloadAndExtractGoModule("golang.org/toolchain", fmt.Sprintf(
			"v0.0.1-go%s.%s-%s",
			toolchain, runtime.GOOS, runtime.GOARCH,
		)),
		Target: "/toolchain",
	})

	for _, require := range modFile.Require {
		modPath := formatPath(require.Mod.Path)
		dirs = append(dirs, common.DirectiveAddFile{
			Filename:   fmt.Sprintf("/modules/%s/@v/%s.zip", formatPath(require.Mod.Path), require.Mod.Version),
			Definition: downloadGoModule(modPath, require.Mod.Version),
		})

		filename := fmt.Sprintf("/modules/%s/@v/%s.mod", formatPath(require.Mod.Path), require.Mod.Version)
		dirs = append(dirs, common.DirectiveAddFile{
			Filename:   filename,
			Definition: downloadGoModuleFile(modPath, require.Mod.Version),
		})
	}

	toolchainBase := fmt.Sprintf("/toolchain/golang.org/toolchain@v0.0.1-go%s.%s-%s", toolchain, runtime.GOOS, runtime.GOARCH)

	dirs = append(dirs, common.DirectiveAddFile{
		Filename: "/build.star",
		Contents: []byte(fmt.Sprintf(`
def main():
	path_ensure("/tmp")
	chdir("/src/%s")
	set_env("GOSUMDB", "off")
	set_env("GOPROXY", "file:///modules")
	# Run the build
	run("%s/bin/go", "run", "./tools/build.go", "-release")
`, modDir, toolchainBase)),
	})

	dirs = append(dirs, common.DirectiveRunCommand{
		Command: fmt.Sprintf("/init -star /build.star"),
	})

	def := builder.Factory.NewBuildVmDefinition(
		dirs,
		nil, nil,
		fmt.Sprintf("/src/%s/release/tinyrange-%s-%s.zip", modDir, runtime.GOOS, runtime.GOARCH),
		1, 1024, config.ArchInvalid, config.HostArchitecture,
		4096,
		"ssh",
		false,
	)

	if _, err := db.Builder().Build(def, common.BuildOptions{
		AlwaysRebuild: true,
	}); err != nil {
		return err
	}

	return nil
}

func main() {
	if err := appMain(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

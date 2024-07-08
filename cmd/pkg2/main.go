package main

import (
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/pprof"
	"strings"
	"time"

	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/database"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/ui"
)

type fileListArray map[string]filesystem.File

// String implements flag.Value.
func (i fileListArray) String() string {
	var ret []string

	for name := range i {
		ret = append(ret, name)
	}

	return "[" + strings.Join(ret, ", ") + "]"
}

func (i fileListArray) Set(value string) error {
	if k, v, ok := strings.Cut(value, "="); ok {
		i[k] = filesystem.NewLocalFile(v)

		return nil
	} else {
		base := filepath.Base(value)

		i[base] = filesystem.NewLocalFile(value)

		return nil
	}
}

var (
	makeList   = flag.String("make", "", "make a container from a list of packages")
	builder    = flag.String("builder", "", "specify a builder to use for making containers")
	buildTags  = flag.String("tags", "level1", "specify a list of tags to build the container with")
	test       = flag.Bool("test", false, "load all container builders")
	cpuprofile = flag.String("cpuprofile", "", "write cpu profile to file")
	memprofile = flag.String("memprofile", "", "write memory profile to this file")
	rebuild    = flag.Bool("rebuild", false, "rebuild all starlark-defined build definitions")
	noParallel = flag.Bool("no-parallel", false, "disable parallel initialization of container builders")
	script     = flag.String("script", "", "load a script rather than providing a interface for the package database")
	httpAddr   = flag.String("http", "", "if specified run a web frontend listening on this address")
	fileList   = make(fileListArray)
	buildDir   = flag.String("build-dir", common.GetDefaultBuildDir(), "specify the directory that will be used for build files")
	buildDef   = flag.String("build", "", "build a single definition defined somewhere in a loaded file")
)

func pkg2Main() error {
	flag.Var(&fileList, "file", "specify files that will be accessible to scripts")
	flag.Parse()

	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			return err
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	if err := common.Ensure(*buildDir, fs.ModePerm); err != nil {
		return err
	}

	db := database.New(*buildDir)

	db.RebuildUserDefinitions = *rebuild

	for _, arg := range flag.Args() {
		if err := db.LoadFile(arg); err != nil {
			return err
		}
	}

	if *script != "" {
		if err := db.RunScript(*script, fileList); err != nil {
			return err
		}
	} else if *buildDef != "" {
		res, err := db.BuildByName(*buildDef)
		if err != nil {
			return err
		}

		ctx := db.NewBuildContext(nil)

		filename, err := ctx.FilenameFromDigest(res.Digest())
		if err != nil {
			return err
		}

		slog.Info("result", "filename", filename)
	} else {
		if *test {
			start := time.Now()

			if err := db.LoadAll(!*noParallel); err != nil {
				return err
			}

			slog.Info("loaded all container builders", "duration", time.Since(start))
		}

		if *builder != "" {
			builder, err := db.GetBuilder(*builder)
			if err != nil {
				return err
			}

			if *makeList != "" {
				pkgs := strings.Split((*makeList), ",")

				var queries []common.PackageQuery
				for _, pkg := range pkgs {
					query, err := common.ParsePackageQuery(pkg)
					if err != nil {
						return err
					}

					queries = append(queries, query)
				}

				tags := strings.Split((*buildTags), ",")

				plan, err := builder.Plan(queries, common.TagList(tags))
				if err != nil {
					return err
				}

				contents, err := database.EmitDockerfile(plan)
				if err != nil {
					return err
				}

				if _, err := fmt.Fprintf(os.Stdout, "%s\n", contents); err != nil {
					return err
				}
			} else {
				flag.Usage()
			}
		} else if *httpAddr != "" {
			ui := ui.New(*httpAddr, db)

			return ui.ListenAndServe()
		} else if !*test {
			flag.Usage()
		}
	}

	if *memprofile != "" {
		f, err := os.Create(*memprofile)
		if err != nil {
			return err
		}
		pprof.WriteHeapProfile(f)
		f.Close()
	}

	return nil
}

func main() {
	if err := pkg2Main(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

package main

import (
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/pprof"
	"strings"
	"time"

	"github.com/tinyrange/tinyrange/pkg/buildinfo"
	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/database"
	_ "github.com/tinyrange/tinyrange/pkg/platform"
	"github.com/tinyrange/tinyrange/pkg/ui"
)

type fileListArray map[string]common.File

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
		i[k] = common.NewLocalFile(v)

		return nil
	} else {
		base := filepath.Base(value)

		i[base] = common.NewLocalFile(value)

		return nil
	}
}

var (
	makeList     = flag.String("make", "", "make a container from a list of packages")
	builder      = flag.String("builder", "", "specify a builder to use for making containers")
	buildTags    = flag.String("tags", "level1", "specify a list of tags to build the container with")
	test         = flag.Bool("test", false, "load all container builders")
	cpuprofile   = flag.String("cpuprofile", "", "write cpu profile to file")
	memprofile   = flag.String("memprofile", "", "write memory profile to this file")
	rebuild      = flag.Bool("rebuild", false, "rebuild all starlark-defined build definitions")
	noParallel   = flag.Bool("no-parallel", false, "disable parallel initialization of container builders")
	script       = flag.String("script", "", "load a script rather than providing a interface for the package database")
	httpAddr     = flag.String("http", "", "if specified run a web frontend listening on this address")
	fileList     = make(fileListArray)
	buildDir     = flag.String("build-dir", common.GetDefaultBuildDir(), "specify the directory that will be used for build files")
	buildDef     = flag.String("build", "", "build a single definition defined somewhere in a loaded file")
	buildOutput  = flag.String("o", "", "copy the build output to this path")
	printVersion = flag.Bool("version", false, "print the version information")
)

func pkg2Main() error {
	flag.Var(&fileList, "file", "specify files that will be accessible to scripts")
	flag.Parse()

	if *printVersion {
		fmt.Printf("TinyRange pkg2 version: %s\nThe University of Queensland\n", buildinfo.VERSION)
		return nil
	}

	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			return err
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	slog.Info("TinyRange pkg2", "version", buildinfo.VERSION)

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
		if err := db.RunScript(*script, fileList, *buildOutput); err != nil {
			return err
		}
	} else if *buildDef != "" {
		res, err := db.BuildByName(*buildDef, common.BuildOptions{
			AlwaysRebuild: *rebuild,
		})
		if err != nil {
			return err
		}

		if *buildOutput != "" {
			out, err := os.Create(*buildOutput)
			if err != nil {
				return err
			}
			defer out.Close()

			fh, err := res.Open()
			if err != nil {
				return err
			}

			if _, err := io.Copy(out, fh); err != nil {
				return err
			}
		} else {
			ctx := db.NewBuildContext(nil)

			filename, err := ctx.FilenameFromDigest(res.Digest())
			if err != nil {
				return err
			}

			slog.Info("result", "filename", filename)
		}
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

				plan, err := builder.Plan(queries, common.TagList(tags), common.PlanOptions{})
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

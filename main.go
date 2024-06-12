package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"runtime/pprof"
	"strings"

	"github.com/tinyrange/pkg2/v2/common"
	"github.com/tinyrange/pkg2/v2/database"
)

var (
	makeList   = flag.String("make", "", "make a container from a list of packages")
	builder    = flag.String("builder", "", "specify a builder to use for making containers")
	test       = flag.Bool("test", false, "load all container builders")
	cpuprofile = flag.String("cpuprofile", "", "write cpu profile to file")
	memprofile = flag.String("memprofile", "", "write memory profile to this file")
	rebuild    = flag.Bool("rebuild", false, "rebuild all starlark-defined build definitions")
	noParallel = flag.Bool("noparallel", false, "disable parallel initialization of container builders")
)

func main() {
	flag.Parse()

	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			log.Fatal(err)
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	db := database.New()

	db.RebuildUserDefinitions = *rebuild

	for _, arg := range flag.Args() {
		if err := db.LoadScript(arg); err != nil {
			log.Fatal(err)
		}
	}

	if *test {
		if err := db.LoadAll(!*noParallel); err != nil {
			log.Fatal(err)
		}
	}

	if *builder != "" {
		builder, err := db.GetBuilder(*builder)
		if err != nil {
			log.Fatal(err)
		}

		if *makeList != "" {
			pkgs := strings.Split((*makeList), ",")

			var queries []common.PackageQuery
			for _, pkg := range pkgs {
				query, err := common.ParsePackageQuery(pkg)
				if err != nil {
					log.Fatal(err)
				}

				queries = append(queries, query)
			}

			plan, err := builder.Plan(queries)
			if err != nil {
				log.Fatal(err)
			}

			contents, err := database.EmitDockerfile(plan)
			if err != nil {
				log.Fatal(err)
			}

			if _, err := fmt.Fprintf(os.Stdout, "%s\n", contents); err != nil {
				log.Fatal(err)
			}
		} else {
			flag.Usage()
		}
	} else if !*test {
		flag.Usage()
	}

	if *memprofile != "" {
		f, err := os.Create(*memprofile)
		if err != nil {
			log.Fatal(err)
		}
		pprof.WriteHeapProfile(f)
		f.Close()
		return
	}
}

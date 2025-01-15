package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/tinyrange/tinyrange/pkg/build2"
	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/hash"
	"go.starlark.net/starlark"
)

type basicBuildDefinitionParams struct {
	Name       string
	WaitTime   int // in milliseconds
	ExpireTime int // in milliseconds
	Children   []common.BuildDefinition
}

func (p basicBuildDefinitionParams) SerializableType() string { return "basic" }

var (
	_ hash.SerializableValue = &basicBuildDefinitionParams{}
)

type basicBuildDefinition struct {
	params basicBuildDefinitionParams
}

// ToStarlark implements common.BuildDefinition.
func (d *basicBuildDefinition) ToStarlark(ctx common.BuildContext, artifact common.BuildArtifact) (starlark.Value, error) {
	return starlark.None, fmt.Errorf("not implemented")
}

// String implements BuildDefinition.
func (d *basicBuildDefinition) String() string {
	return d.params.Name
}

// Create implements BuildDefinition.
func (d *basicBuildDefinition) Create(params hash.SerializableValue) hash.Definition {
	return &basicBuildDefinition{params: params.(basicBuildDefinitionParams)}
}

// Params implements BuildDefinition.
func (d *basicBuildDefinition) Params() hash.SerializableValue {
	return d.params
}

// SerializableType implements BuildDefinition.
func (d *basicBuildDefinition) SerializableType() string {
	return "basic"
}

// NeedsBuild implements BuildDefinition.
func (d *basicBuildDefinition) NeedsBuild(ctx common.BuildContext) (bool, error) {
	lastBuild := ctx.LastBuild()

	if lastBuild.IsZero() {
		return true, nil
	}

	if d.params.ExpireTime > 0 && time.Since(lastBuild) > time.Duration(d.params.ExpireTime)*time.Millisecond {
		return true, nil
	}

	return false, nil
}

// Dependencies implements BuildDefinition.
func (d *basicBuildDefinition) Dependencies() ([]common.BuildDefinition, error) {
	return d.params.Children, nil
}

// Build implements BuildDefinition.
func (d *basicBuildDefinition) Build(ctx common.BuildContext) error {
	waitTime := time.Duration(d.params.WaitTime) * time.Millisecond
	ctx.Describe("waiting for %s", waitTime)

	time.Sleep(waitTime)

	out, err := ctx.CreateFile("txt")
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := fmt.Fprintf(out, "- %s\n", ctx.DefinitionHash()); err != nil {
		return err
	}

	for _, child := range d.params.Children {
		ctx.Describe("waiting for child %s", child.String())
		art, err := ctx.BuildChild(child)
		if err != nil {
			return err
		}

		childOut, err := art.OpenFile("txt")
		if err != nil {
			return err
		}

		scanner := bufio.NewScanner(childOut)

		for scanner.Scan() {
			if _, err := fmt.Fprintf(out, "  %s\n", scanner.Text()); err != nil {
				return err
			}
		}
	}

	return nil
}

var (
	_ common.BuildDefinition = &basicBuildDefinition{}
)

func newBasicBuildDefinition(name string, waitTime int, expireTime int, children ...common.BuildDefinition) *basicBuildDefinition {
	return &basicBuildDefinition{
		params: basicBuildDefinitionParams{
			Name:       name,
			WaitTime:   waitTime,
			ExpireTime: expireTime,
			Children:   children,
		},
	}
}

var (
	jobs           = flag.Int("jobs", 1, "number of jobs to run in parallel")
	nodes          = flag.Int("nodes", 100, "number of nodes in the graph")
	edges          = flag.Int("edges", 100, "number of edges in the graph")
	height         = flag.Int("height", 30, "height of the logger")
	buildDir       = flag.String("build-dir", "", "set to use a real build directory")
	generate       = flag.String("generate", "", "generate a graph to a file")
	load           = flag.String("load", "", "load a graph from a file")
	garbageCollect = flag.Bool("gc", false, "run garbage collection")
)

func appMain() error {
	flag.Parse()

	hash.RegisterType(&basicBuildDefinition{})

	if *generate != "" {
		slog.Info("generating graph")

		graph, root, err := generateRandomDAG(*nodes, *edges)
		if err != nil {
			return err
		}

		slog.Info("saving graph to file")

		out, err := os.Create(*generate)
		if err != nil {
			return err
		}
		defer out.Close()

		if err := saveGraph(out, graph, root); err != nil {
			return err
		}

		return nil
	}

	var rootDef common.BuildDefinition

	if *load != "" {
		f, err := os.Open(*load)
		if err != nil {
			return err
		}
		defer f.Close()

		rootDef, err = loadGraph(f)
		if err != nil {
			return err
		}
	} else {
		slog.Info("generating graph")

		graph, root, err := generateRandomDAG(*nodes, *edges)
		if err != nil {
			return err
		}

		slog.Info("converting graph to build definition")

		buildGraph, err := graphToBuildDefinition(graph)
		if err != nil {
			return err
		}

		slog.Info("generated graph")

		rootDef = buildGraph[root]
	}

	logger := build2.NewSimpleLogger()

	var buildMut filesystem.MutableDirectory

	if *buildDir != "" {
		if err := os.MkdirAll(*buildDir, 0755); err != nil {
			return err
		}

		buildMut = filesystem.NewLocalMutableDirectory(*buildDir)

		if *garbageCollect {

			builder := build2.New(buildMut, *jobs, logger.Group("build"))

			hashes, err := builder.GarbageCollect(time.Now().Add(-time.Minute * 10))
			if err != nil {
				return err
			}

			for _, hash := range hashes {
				fmt.Printf("deleted %s\n", hash)
			}

			return nil
		}
	} else {
		buildMut = filesystem.NewMemoryDirectory()
	}

	builder := build2.New(buildMut, *jobs, logger.Group("build"))

	go func() {
		if err := logger.Run(os.Stdout); err != nil {
			slog.Error("logger error", "err", err)
		}
	}()
	defer logger.Close()

	art, err := builder.Build(rootDef, common.BuildOptions{})
	if err != nil {
		return err
	}

	f, err := art.OpenFile("txt")
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := io.Copy(os.Stdout, f); err != nil {
		return err
	}

	return nil
}

func main() {
	if err := appMain(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

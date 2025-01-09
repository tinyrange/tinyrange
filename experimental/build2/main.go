package main

import (
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
)

type basicBuildDefinitionParams struct {
	Name       string
	SleepTime  int
	ExpireTime int
	Children   []build2.BuildDefinition
}

// SerializableType implements hash.SerializableValue.
func (b basicBuildDefinitionParams) SerializableType() string { return "basicBuildDefinitionParams" }

var (
	_ hash.SerializableValue = basicBuildDefinitionParams{}
)

type basicBuildDefinition struct {
	params basicBuildDefinitionParams
}

func (b *basicBuildDefinition) OutputDot(out io.Writer) error {
	added := make(map[build2.BuildDefinition]struct{})

	if _, err := fmt.Fprintf(out, "digraph G {\n"); err != nil {
		return err
	}

	var addNode func(def build2.BuildDefinition) error

	addNode = func(def build2.BuildDefinition) error {
		if _, ok := added[def]; ok {
			return nil
		}

		added[def] = struct{}{}

		// slog.Info("adding node", "def", def)

		if _, err := fmt.Fprintf(out, "  \"%p\" [label=\"%s\"];\n", def, def); err != nil {
			return err
		}

		deps, err := def.Dependencies()
		if err != nil {
			panic(err)
		}

		for _, dep := range deps {
			if _, err := fmt.Fprintf(out, "  \"%p\" -> \"%p\";\n", def, dep); err != nil {
				return err
			}
			addNode(dep)
		}

		return nil
	}

	addNode(b)

	if _, err := fmt.Fprintf(out, "}\n"); err != nil {
		return err
	}

	return nil
}

func (b *basicBuildDefinition) String() string {
	return b.params.Name
}

// Create implements BuildDefinition.
func (b *basicBuildDefinition) Create(params hash.SerializableValue) hash.Definition {
	return &basicBuildDefinition{params: params.(basicBuildDefinitionParams)}
}

// Params implements BuildDefinition.
func (b *basicBuildDefinition) Params() hash.SerializableValue {
	return b.params
}

// SerializableType implements BuildDefinition.
func (b *basicBuildDefinition) SerializableType() string {
	return "basicBuildDefinition"
}

// Build implements BuildDefinition.
func (b *basicBuildDefinition) Build(ctx build2.BuildContext) error {
	ctx.Logf("[#] starting name=%s", b.params.Name)

	for i, child := range b.params.Children {
		if _, err := ctx.BuildChild(child, build2.BuildOptions{}); err != nil {
			return err
		}
		ctx.Logf("[#] finished child=%s (%d/%d)", child, i+1, len(b.params.Children))
	}

	ctx.Logf("[#] building name=%s", b.params.Name)

	time.Sleep(time.Duration(b.params.SleepTime) * time.Millisecond)

	out, err := ctx.CreateOutput("output")
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := out.Write([]byte("hello world\n")); err != nil {
		return fmt.Errorf("failed to write output: %w", err)
	}

	if b.params.ExpireTime > 0 {
		ctx.SetExpireTime(time.Now().Add(time.Duration(b.params.ExpireTime) * time.Millisecond))
	}

	ctx.Logf("[#] finished name=%s", b.params.Name)

	return nil
}

// Dependencies implements BuildDefinition.
func (b *basicBuildDefinition) Dependencies() ([]build2.BuildDefinition, error) {
	return b.params.Children, nil
}

var (
	_ build2.BuildDefinition = &basicBuildDefinition{}
)

func newBasicBuildDefinition(
	name string,
	expireTime time.Duration,
	sleepTime time.Duration,
	children ...build2.BuildDefinition,
) *basicBuildDefinition {
	return &basicBuildDefinition{
		params: basicBuildDefinitionParams{
			Name:       name,
			SleepTime:  int(sleepTime.Milliseconds()),
			ExpireTime: int(expireTime.Milliseconds()),
			Children:   children,
		},
	}
}

// Helper function to check if a child with a given name is already in the children slice
func containsChild(children []build2.BuildDefinition, childName string) bool {
	for _, child := range children {
		if child.(*basicBuildDefinition).params.Name == childName {
			return true
		}
	}
	return false
}

// Helper function to remove an element from a slice of strings
func removeFromSlice(slice []string, element string) []string {
	index := -1
	for i, name := range slice {
		if name == element {
			index = i
			break
		}
	}
	if index == -1 {
		return slice
	}
	return append(slice[:index], slice[index+1:]...)
}

var (
	buildPath = flag.String("build-dir", "", "The build directory")
	jobs      = flag.Int("jobs", 1, "The number of parallel jobs")
)

func appMain() error {
	flag.Parse()

	hash.RegisterType(&basicBuildDefinition{})

	buildDir := filesystem.NewMemoryDirectory()
	if *buildPath != "" {
		if err := common.Ensure(*buildPath, os.ModePerm); err != nil {
			return err
		}

		buildDir = filesystem.NewLocalMutableDirectory(*buildPath)
	}

	// tui := build2.NewSimpleLogger()
	// tui := build2.NewBuildLogger(32)
	tui := build2.NewEventDrivenLogger(30)

	builder := build2.NewBuilder(buildDir, *jobs, tui)

	item2 := newBasicBuildDefinition("item2", 0, 100*time.Millisecond)

	item3 := newBasicBuildDefinition("item3", 0, 100*time.Millisecond)

	defTree := newBasicBuildDefinition("top", 0, 200*time.Millisecond,
		newBasicBuildDefinition("topIt2", 0, 400*time.Millisecond, item2),
		newBasicBuildDefinition("long", 0, 800*time.Millisecond,
			newBasicBuildDefinition("longChild1", 0, 200*time.Millisecond),
			newBasicBuildDefinition("longChild2", 100*time.Millisecond, 200*time.Millisecond, item2),
			newBasicBuildDefinition("longChild3", 0, 200*time.Millisecond),
			newBasicBuildDefinition("longChild4", 0, 200*time.Millisecond, item3),
			newBasicBuildDefinition("longChild5", 0, 200*time.Millisecond, item3),
		),
		item2,
	)

	if _, err := builder.Build(defTree, build2.BuildOptions{}); err != nil {
		return err
	}

	// dumpTree(builder, res, nil, "")

	deleted, err := builder.GarbageCollect(time.Now().Add(-2 * time.Minute))
	if err != nil {
		return err
	}
	for _, hash := range deleted {
		slog.Info("deleted", "hash", hash)
		if err := buildDir.Unlink(string(hash)); err != nil {
			return err
		}
	}

	return nil
}

func main() {
	if err := appMain(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

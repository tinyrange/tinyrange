package main

import (
	"flag"
	"fmt"
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

func newBasicBuildDefinition(name string, expireTime time.Duration, sleepTime time.Duration, children ...build2.BuildDefinition) *basicBuildDefinition {
	return &basicBuildDefinition{
		params: basicBuildDefinitionParams{
			Name:       name,
			SleepTime:  int(sleepTime.Milliseconds()),
			ExpireTime: int(expireTime.Milliseconds()),
			Children:   children,
		},
	}
}

// func dumpTree(b build2.Builder, art build2.BuildArtifact, info *build2.DependencyInfo, prefix string) {
// 	def, err := b.DefinitionFromArtifact(art)
// 	if err != nil {
// 		fmt.Fprintf(os.Stderr, "%sfailed to get definition: %v\n", prefix, err)
// 		return
// 	}

// 	usedCache := "fresh"
// 	if info != nil && info.UsedCache {
// 		usedCache = "cache"
// 	}

// 	fmt.Fprintf(os.Stderr, "[%s] %s%s [%s, %s]\n", formatHash(art.Hash()), prefix, def, art.Receipt().BuildDuration, usedCache)
// 	for _, dep := range art.Receipt().Dependencies {
// 		child, err := b.ArtifactFromHash(dep.Hash)
// 		if err != nil {
// 			fmt.Fprintf(os.Stderr, "%sfailed to get child: %v\n", prefix, err)
// 			continue
// 		}

// 		dumpTree(b, child, dep, prefix+"  ")
// 	}
// }

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

	// tui := NewSimpleLogger()
	tui := build2.NewBuildLogger(32)

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

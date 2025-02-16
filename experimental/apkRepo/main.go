package main

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/tinyrange/tinyrange/pkg/archive"
	"github.com/tinyrange/tinyrange/pkg/archive2"
	"github.com/tinyrange/tinyrange/pkg/build2"
	"github.com/tinyrange/tinyrange/pkg/builder"
	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/database"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/hash"
	"github.com/tinyrange/tinyrange/pkg/path"
	"go.starlark.net/starlark"
)

type SimpleBuildDefinition struct {
	Name   string
	params hash.SerializableValue
	build  func(ctx common.BuildContext, params hash.SerializableValue) error
}

// Build implements common.BuildDefinition.
func (s *SimpleBuildDefinition) Build(ctx common.BuildContext) error {
	return s.build(ctx, s.params)
}

// Create implements common.BuildDefinition.
func (s *SimpleBuildDefinition) Create(params hash.SerializableValue) hash.Definition {
	return &SimpleBuildDefinition{
		Name:   s.Name,
		params: params,
		build:  s.build,
	}
}

// Dependencies implements common.BuildDefinition.
func (s *SimpleBuildDefinition) Dependencies() ([]common.BuildDefinition, error) {
	return nil, nil
}

// NeedsBuild implements common.BuildDefinition.
func (s *SimpleBuildDefinition) NeedsBuild(ctx common.BuildContext) (bool, error) {
	return false, nil
}

// Params implements common.BuildDefinition.
func (s *SimpleBuildDefinition) Params() hash.SerializableValue {
	return s.params
}

// SerializableType implements common.BuildDefinition.
func (s SimpleBuildDefinition) SerializableType() string {
	return "Simple_" + s.Name
}

// String implements common.BuildDefinition.
func (s *SimpleBuildDefinition) String() string {
	return s.Name
}

// ToStarlark implements common.BuildDefinition.
func (s *SimpleBuildDefinition) ToStarlark(artifact common.BuildArtifact) (starlark.Value, error) {
	return nil, fmt.Errorf("not implemented")
}

var (
	_ common.BuildDefinition = &SimpleBuildDefinition{}
)

func NewSimpleBuildDefinition(name string, build func(ctx common.BuildContext, params hash.SerializableValue) error) common.BuildDefinition {
	s := &SimpleBuildDefinition{
		Name:  name,
		build: build,
	}

	hash.RegisterType(s)

	return s
}

func filesystemFromArchiveDefinition(ctx common.BuildContext, def common.BuildDefinition) (filesystem.Directory, error) {
	art, err := ctx.BuildChild(def)
	if err != nil {
		return nil, err
	}

	defFile, err := art.Default()
	if err != nil {
		return nil, err
	}

	ark, err := archive.ReadArchiveFromFile(defFile)
	if err != nil {
		return nil, err
	}

	top := filesystem.NewMemoryDirectory()

	if err := archive.ExtractArchive(ark, top); err != nil {
		return nil, err
	}

	return top, nil
}

type apkEntry struct {
	// C: - file checksum, see below
	Checksum string
	// P: - package name (corresponds to pkgname in PKGINFO)
	Package string
	// V: - package version (corresponds to pkgver in PKGINFO)
	Version string
	// A: - architecture (corresponds to arch in PKGINFO), optional
	Architecture string
	// S: - size of entire package, integer
	Size int
	// I: - installed size, integer (corresponds to size in PKGINFO)
	InstalledSize int
	// T: - description (corresponds to pkgdesc in PKGINFO)
	Description string
	// U: - url (corresponds to url in PKGINFO)
	URL string
	// L: - license (corresponds to license in PKGINFO)
	License string
	// o: - origin (corresponds to origin in PKGINFO), optional
	Origin string
	// m: - maintainer (corresponds to maintainer in PKGINFO), optional
	Maintainer string
	// t: - build time (corresponds to builddate in PKGINFO), optional
	BuildTime time.Time
	// c: - commit (corresponds to commit in PKGINFO), optional
	Commit string
	// k: - provider priority, integer (corresponds to provider_priority in PKGINFO), optional
	ProviderPriority int
	// D: - dependencies (corresponds to depend in PKGINFO, concatenated by spaces into a single line)
	Dependencies []string
	// p: - provides (corresponds to provides in PKGINFO, concatenated by spaces into a single line)
	Provides []string
	// i: - install if (corresponds to install_if in PKGINFO, concatenated by spaces into a single line)
	InstallIf []string
}

func (a apkEntry) String() string {
	return fmt.Sprintf("%s=%s-%s", a.Package, a.Version, a.Architecture)
}

func parseApkIndex(f filesystem.File) ([]apkEntry, error) {
	fh, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer fh.Close()

	var ret []apkEntry
	var current apkEntry

	scanner := bufio.NewScanner(fh)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if current.Package != "" {
				ret = append(ret, current)
			}
			current = apkEntry{}
			continue
		}

		tokens := strings.Split(line, ":")
		key, value := tokens[0], strings.Join(tokens[1:], ":")

		switch key {
		case "C":
			current.Checksum = value
		case "P":
			current.Package = value
		case "V":
			current.Version = value
		case "A":
			current.Architecture = value
		case "S":
			fmt.Sscanf(value, "%d", &current.Size)
		case "I":
			fmt.Sscanf(value, "%d", &current.InstalledSize)
		case "T":
			current.Description = value
		case "U":
			current.URL = value
		case "L":
			current.License = value
		case "o":
			current.Origin = value
		case "m":
			current.Maintainer = value
		case "t":
			// time is stored as a unix timestamp
			var ts int64
			fmt.Sscanf(value, "%d", &ts)
			current.BuildTime = time.Unix(ts, 0)
		case "c":
			current.Commit = value
		case "k":
			fmt.Sscanf(value, "%d", &current.ProviderPriority)
		case "D":
			current.Dependencies = strings.Fields(value)
		case "p":
			current.Provides = strings.Fields(value)
		case "i":
			current.InstallIf = strings.Fields(value)
		default:
			return nil, fmt.Errorf("unknown key %q", key)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	if current.Package != "" {
		ret = append(ret, current)
	}

	return ret, nil
}

type DownloadEntryParams struct {
	Repo                    string
	Package                 string
	Version                 string
	Architecture            string
	IndexVersion            string
	IndexOutputDirectory    string
	ContentsOutputDirectory string
}

func (d DownloadEntryParams) SerializableType() string { return "DownloadEntryParams" }

var (
	_ hash.SerializableValue = DownloadEntryParams{}
)

var downloadEntry = NewSimpleBuildDefinition("downloadEntry", func(ctx common.BuildContext, p hash.SerializableValue) error {
	params := p.(DownloadEntryParams)

	url := fmt.Sprintf("%s/%s/%s-%s.apk", params.Repo, params.Architecture, params.Package, params.Version)

	downloaded := builder.Factory.NewFetchHttpBuildDefinition(url, 0, nil)

	art, err := ctx.BuildChild(downloaded)
	if err != nil {
		return err
	}

	defFile, err := art.Default()
	if err != nil {
		return err
	}

	// assume the file is .tar.gz
	fh, err := defFile.Open()
	if err != nil {
		return err
	}

	reader, err := gzip.NewReader(fh)
	if err != nil {
		return err
	}

	tarReader := tar.NewReader(reader)

	indexFilename := path.Native.Join(
		params.IndexOutputDirectory,
		fmt.Sprintf("%s/%s/%s.index", params.Architecture, params.Package, params.Version),
	)

	if err := common.Ensure(path.Native.Dir(indexFilename), os.ModePerm); err != nil {
		return err
	}

	indexFile, err := os.Create(indexFilename)
	if err != nil {
		return err
	}
	defer indexFile.Close()

	contentsFile, err := os.Create(path.Native.Join(
		params.ContentsOutputDirectory,
		fmt.Sprintf("%s-%s-%s.contents", params.Package, params.Version, params.Architecture),
	))
	if err != nil {
		return err
	}
	defer contentsFile.Close()

	ark, err := archive2.NewArchiveWriter(indexFile, contentsFile)
	if err != nil {
		return err
	}

	for {
		header, err := tarReader.Next()
		if err != nil {
			if err == io.EOF {
				break
			} else {
				return err
			}
		}

		ent := &archive2.EntryFactory{}

		switch header.Typeflag {
		case tar.TypeReg:
			ent = ent.Kind(archive2.EntryKindRegular)
		case tar.TypeDir:
			ent = ent.Kind(archive2.EntryKindDirectory)
		case tar.TypeSymlink:
			ent = ent.Kind(archive2.EntryKindSymlink)
		case tar.TypeLink:
			ent = ent.Kind(archive2.EntryKindHardlink)
		default:
			return fmt.Errorf("unknown typeflag %v", header.Typeflag)
		}

		info := header.FileInfo()

		if err := ark.WriteEntry(
			ent.Name(header.Name).
				Linkname(header.Linkname).
				Mode(info.Mode()).
				Owner(header.Uid, header.Gid).
				Size(header.Size).
				ModTime(info.ModTime()),
			tarReader,
		); err != nil {
			return err
		}
	}

	return nil
})

type TopLevelParams struct {
	Repo           string
	Architecture   string
	IndexOutput    string
	ContentsOutput string
}

func (t TopLevelParams) SerializableType() string { return "TopLevelParams" }

var (
	_ hash.SerializableValue = TopLevelParams{}
)

var topLevelBuild = NewSimpleBuildDefinition("topLevelBuild", func(ctx common.BuildContext, p hash.SerializableValue) error {
	params := p.(TopLevelParams)

	// Download the top level index file.
	indexFile := builder.Factory.NewReadArchiveBuildDefinition(
		builder.Factory.NewFetchHttpBuildDefinition(fmt.Sprintf("%s/%s/APKINDEX.tar.gz", params.Repo, params.Architecture), 2*time.Hour, nil),
		".tar.gz",
	)

	index, err := filesystemFromArchiveDefinition(ctx, indexFile)
	if err != nil {
		return err
	}

	f, err := filesystem.OpenPath(index, "APKINDEX")
	if err != nil {
		return err
	}

	entries, err := parseApkIndex(f)
	if err != nil {
		return err
	}

	// split entries into groups of 1000
	// this is to prevent the number of threads from getting too high
	var groups [][]apkEntry
	for i := 0; i < len(entries); i += 1000 {
		end := i + 1000
		if end > len(entries) {
			end = len(entries)
		}
		groups = append(groups, entries[i:end])
	}

	for i, group := range groups {
		var downloadEntries []common.BuildDefinition

		for _, entry := range group {
			downloadEntries = append(downloadEntries, downloadEntry.Create(DownloadEntryParams{
				Repo:                    params.Repo,
				Package:                 entry.Package,
				Version:                 entry.Version,
				Architecture:            entry.Architecture,
				IndexOutputDirectory:    params.IndexOutput,
				ContentsOutputDirectory: params.ContentsOutput,
				IndexVersion:            "1",
			}).(common.BuildDefinition))
		}

		if err := ctx.PrenotifyChildren(downloadEntries); err != nil {
			return err
		}

		for _, entry := range downloadEntries {
			if _, err := ctx.BuildChild(entry); err != nil {
				return err
			}
		}

		slog.Info("built group", "index", i, "size", len(group))
	}

	return nil
})

var (
	repo           = flag.String("repo", "https://packages.wolfi.dev/os", "Alpine repository to use")
	buildDir       = flag.String("build-dir", "local/apkRepo", "Directory to store build files")
	arch           = flag.String("arch", "x86_64", "Architecture to build for")
	indexOutput    = flag.String("index-output", "local/apkRepo/index", "Directory to store index files")
	contentsOutput = flag.String("contents-output", "local/apkRepo/contents", "Directory to store contents files")
	jobs           = flag.Int("jobs", 1, "Number of jobs to run in parallel")
	maxThreads     = flag.Int("max-threads", 10000, "Set the maximum number of threads")
)

func appMain() error {
	if err := common.EnableVerbose(); err != nil {
		return err
	}

	flag.Parse()

	debug.SetMaxThreads(*maxThreads)
	// feature.ToggleFeature(feature.FeatureTokenLockerDebug)

	db, err := database.New(func(pd common.PackageDatabase) (common.Builder, error) {
		if err := common.Ensure(*buildDir, os.ModePerm); err != nil {
			return nil, err
		}

		mutBuildDir := filesystem.NewLocalMutableDirectory(*buildDir)

		logger := build2.NewSimpleLogger()

		return build2.New(mutBuildDir, pd, *jobs, logger.Group("root")), nil
	})
	if err != nil {
		return err
	}

	if err := common.Ensure(*indexOutput, os.ModePerm); err != nil {
		return err
	}

	if err := common.Ensure(*contentsOutput, os.ModePerm); err != nil {
		return err
	}

	if _, err := db.Builder().Build(topLevelBuild.Create(TopLevelParams{
		Repo:           *repo,
		Architecture:   *arch,
		IndexOutput:    *indexOutput,
		ContentsOutput: *contentsOutput,
	}).(common.BuildDefinition), common.BuildOptions{
		AlwaysRebuild: true,
	}); err != nil {
		return err
	}

	return fmt.Errorf("not implemented")
}

func main() {
	if err := appMain(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

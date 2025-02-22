package main

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	_ "net/http/pprof"
	"os"
	"regexp"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/schollz/progressbar/v3"
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

const (
	outputPrefix = "output."
)

type filesystemOutputFileHandle struct {
	filesystem.WritableFileHandle

	mut filesystem.MutableFile
}

// GetHostFilename implements OutputFileHandle.
func (f *filesystemOutputFileHandle) GetHostFilename() (string, error) {
	return filesystem.GetHostFilename(f.mut)
}

type splitBuildDirectory struct {
	fs              *splitBuildFilesystem
	hash            hash.Hash
	definitionFile  filesystem.MutableFile
	receptFile      filesystem.MutableFile
	outputDirectory filesystem.MutableDirectory
}

// CreateOutputFile implements build2.BuildCacheDirectory.
func (s *splitBuildDirectory) CreateOutputFile(name string) (build2.OutputFileHandle, error) {
	if s.outputDirectory == nil {
		out, err := s.fs.getOutputDirectory(s.hash)
		if err != nil {
			return nil, err
		}
		s.outputDirectory = out
	}

	file, err := s.outputDirectory.Create(outputPrefix+name, nil)
	if err != nil {
		return nil, err
	}

	mut, ok := file.(filesystem.MutableFile)
	if !ok {
		return nil, fmt.Errorf("file %T is not mutable", file)
	}

	mutHandle, err := mut.OpenMut()
	if err != nil {
		return nil, err
	}

	return &filesystemOutputFileHandle{
		WritableFileHandle: mutHandle,
		mut:                mut,
	}, nil
}

// GetOutputFile implements build2.BuildCacheDirectory.
func (s *splitBuildDirectory) GetOutputFile(name string) (filesystem.File, error) {
	if s.outputDirectory == nil {
		out, err := s.fs.getOutputDirectory(s.hash)
		if err != nil {
			return nil, err
		}
		s.outputDirectory = out
	}

	file, err := s.outputDirectory.GetChild(outputPrefix + name)
	if err != nil {
		return nil, err
	}

	return file.File, nil
}

// ReadDefinition implements build2.BuildCacheDirectory.
func (s *splitBuildDirectory) ReadDefinition() ([]byte, error) {
	fh, err := s.definitionFile.Open()
	if err != nil {
		return nil, err
	}
	defer fh.Close()

	return io.ReadAll(fh)
}

// ReadReceipt implements build2.BuildCacheDirectory.
func (s *splitBuildDirectory) ReadReceipt() ([]byte, error) {
	fh, err := s.receptFile.Open()
	if err != nil {
		return nil, err
	}
	defer fh.Close()

	return io.ReadAll(fh)
}

// WriteDefinition implements build2.BuildCacheDirectory.
func (s *splitBuildDirectory) WriteDefinition(def []byte) error {
	return s.definitionFile.Overwrite(def)
}

// WriteReceipt implements build2.BuildCacheDirectory.
func (s *splitBuildDirectory) WriteReceipt(recept []byte) error {
	return s.receptFile.Overwrite(recept)
}

var (
	_ build2.BuildCacheDirectory = &splitBuildDirectory{}
)

type splitBuildFilesystem struct {
	// definitions are stored as <first 2 chars of hash>/<hash>.definition
	definitionDirectory filesystem.MutableDirectory
	// receipts are stored as <first 2 chars of hash>/<hash>.receipt
	receptDirectory filesystem.MutableDirectory
	// output directories are stored as <first 2 chars of hash>/<hash>/
	outputDirectory filesystem.MutableDirectory
}

func (s *splitBuildFilesystem) getOutputDirectory(hash hash.Hash) (filesystem.MutableDirectory, error) {
	outputTopDir, err := s.outputDirectory.Mkdir(hash.String()[:2])
	if err != nil {
		return nil, err
	}

	outputDir, err := outputTopDir.Mkdir(hash.String()[2:])
	if err != nil {
		return nil, err
	}

	return outputDir, nil
}

// CreateBuildDirectory implements build2.BuildCacheFilesystem.
func (s *splitBuildFilesystem) CreateBuildDirectory(hash hash.Hash) (build2.BuildCacheDirectory, error) {
	defDir, err := s.definitionDirectory.Mkdir(hash.String()[:2])
	if err != nil {
		return nil, err
	}

	receptDir, err := s.receptDirectory.Mkdir(hash.String()[:2])
	if err != nil {
		return nil, err
	}

	// try and get the file. If it doesn't exist then create it.
	var defFile filesystem.File
	defEnt, err := defDir.GetChild(hash.String() + ".definition")
	if errors.Is(err, os.ErrNotExist) {
		// slog.Info("creating definition file", "hash", hash)
		defFile, err = defDir.Create(hash.String()+".definition", nil)
		if err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	} else {
		// slog.Info("definition file exists", "hash", hash)
		defFile = defEnt.File
	}

	defMut, ok := defFile.(filesystem.MutableFile)
	if !ok {
		return nil, fmt.Errorf("definition file is not mutable")
	}

	var receptFile filesystem.File
	receptEnt, err := receptDir.GetChild(hash.String() + ".receipt")
	if errors.Is(err, os.ErrNotExist) {
		// slog.Info("creating receipt file", "hash", hash)
		receptFile, err = receptDir.Create(hash.String()+".receipt", nil)
		if err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	} else {
		// slog.Info("receipt file exists", "hash", hash)
		receptFile = receptEnt.File
	}

	receptMut, ok := receptFile.(filesystem.MutableFile)
	if !ok {
		return nil, fmt.Errorf("receipt file is not mutable")
	}

	return &splitBuildDirectory{
		fs:             s,
		hash:           hash,
		definitionFile: defMut,
		receptFile:     receptMut,
	}, nil
}

var (
	validByte   = regexp.MustCompile(`^[0-9a-f]{2}$`)
	validSha256 = regexp.MustCompile(`^[0-9a-f]{62}$`)
)

// GetAllHashes implements build2.BuildCacheFilesystem.
func (s *splitBuildFilesystem) GetAllHashes() ([]hash.Hash, error) {
	// enumerate the definition directory
	definitions, err := s.definitionDirectory.Readdir()
	if err != nil {
		return nil, err
	}

	var ret []hash.Hash
	for _, def := range definitions {
		if !validByte.MatchString(def.Name) {
			continue
		}

		defDir, ok := def.File.(filesystem.Directory)
		if !ok {
			continue
		}

		defEnts, err := defDir.Readdir()
		if err != nil {
			return nil, err
		}

		for _, defEnt := range defEnts {
			if !validSha256.MatchString(defEnt.Name) {
				continue
			}

			ret = append(ret, hash.Hash(def.Name+defEnt.Name))
		}
	}

	return ret, nil
}

// GetBuildDirectory implements build2.BuildCacheFilesystem.
func (s *splitBuildFilesystem) GetBuildDirectory(hash hash.Hash) (build2.BuildCacheDirectory, error) {
	defDir, err := s.definitionDirectory.GetChild(hash.String()[:2])
	if err != nil {
		// slog.Info("failed to get definition directory", "hash", hash, "err", err)
		return nil, err
	}

	defEnt, ok := defDir.File.(filesystem.Directory)
	if !ok {
		// slog.Info("definition directory is not a directory", "hash", hash)
		return nil, fmt.Errorf("definition directory is not a directory")
	}

	defFile, err := defEnt.GetChild(hash.String() + ".definition")
	if err != nil {
		// slog.Info("failed to get definition file", "hash", hash, "err", err)
		return nil, err
	}

	defFileMut, ok := defFile.File.(filesystem.MutableFile)
	if !ok {
		// slog.Info("definition file is not mutable", "hash", hash)
		return nil, fmt.Errorf("definition file is not mutable")
	}

	receptDir, err := s.receptDirectory.GetChild(hash.String()[:2])
	if err != nil {
		// slog.Info("failed to get receipt directory", "hash", hash, "err", err)
		return nil, err
	}

	receptEnt, ok := receptDir.File.(filesystem.Directory)
	if !ok {
		// slog.Info("receipt directory is not a directory", "hash", hash)
		return nil, fmt.Errorf("receipt directory is not a directory")
	}

	receptFile, err := receptEnt.GetChild(hash.String() + ".receipt")
	if err != nil {
		// slog.Info("failed to get receipt file", "hash", hash, "err", err)
		return nil, err
	}

	receptFileMut, ok := receptFile.File.(filesystem.MutableFile)
	if !ok {
		// slog.Info("receipt file is not mutable", "hash", hash)
		return nil, fmt.Errorf("receipt file is not mutable")
	}

	return &splitBuildDirectory{
		fs:             s,
		hash:           hash,
		definitionFile: defFileMut,
		receptFile:     receptFileMut,
	}, nil
}

// GetHostFilename implements build2.BuildCacheFilesystem.
func (s *splitBuildFilesystem) GetHostFilename() (string, error) {
	return filesystem.GetHostFilename(s.outputDirectory)
}

var (
	_ build2.BuildCacheFilesystem = &splitBuildFilesystem{}
)

func newSplitBuildDirectory(
	defDir filesystem.MutableDirectory,
	receiptDir filesystem.MutableDirectory,
	outputDir filesystem.MutableDirectory,
) build2.BuildCacheFilesystem {
	return &splitBuildFilesystem{
		definitionDirectory: defDir,
		receptDirectory:     receiptDir,
		outputDirectory:     outputDir,
	}
}

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

	pb := progressbar.Default(int64(len(entries)))
	defer pb.Finish()

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

			pb.Add(1)
		}

		slog.Info("built group", "index", i, "size", len(group))
	}

	return nil
})

func migrateToSplitBuildCache(buildDir string, splitDefinitions string, splitReceipts string, threads int) error {
	buildDirEnt := filesystem.NewLocalMutableDirectory(buildDir)
	if err := common.Ensure(splitDefinitions, os.ModePerm); err != nil {
		return err
	}
	defsDirEnt := filesystem.NewLocalMutableDirectory(splitDefinitions)
	if err := common.Ensure(splitReceipts, os.ModePerm); err != nil {
		return err
	}
	receiptsDirEnt := filesystem.NewLocalMutableDirectory(splitReceipts)

	oldFs := build2.NewFilesystemBuildCache(buildDirEnt)
	newFs := newSplitBuildDirectory(defsDirEnt, receiptsDirEnt, buildDirEnt)

	slog.Info("migrating to split build cache")

	hashes, err := oldFs.GetAllHashes()
	if err != nil {
		return err
	}

	slog.Info("migrating", "count", len(hashes))

	pb := progressbar.Default(int64(len(hashes)))

	var wg sync.WaitGroup
	wg.Add(threads)
	hashesChannel := make(chan hash.Hash, threads)
	errorChannel := make(chan error, threads)

	// use a series of worker threads.
	for i := 0; i < threads; i++ {
		go func() {
			defer wg.Done()
			for {
				pb.Add(1)
				hash, ok := <-hashesChannel
				if !ok {
					return
				}

				dir, err := oldFs.GetBuildDirectory(hash)
				if err != nil {
					errorChannel <- err
					return
				}

				def, err := dir.ReadDefinition()
				if errors.Is(err, os.ErrNotExist) {
					// don't create it if the definition doesn't exist
					continue
				} else if err != nil {
					errorChannel <- err
					return
				}

				recept, err := dir.ReadReceipt()
				if errors.Is(err, os.ErrNotExist) {
					// don't create it if the receipt doesn't exist
					continue
				} else if err != nil {
					errorChannel <- err
					return
				}

				newDir, err := newFs.CreateBuildDirectory(hash)
				if err != nil {
					errorChannel <- err
					return
				}

				if err := newDir.WriteDefinition(def); err != nil {
					errorChannel <- err
					return
				}

				if err := newDir.WriteReceipt(recept); err != nil {
					errorChannel <- err
					return
				}
			}
		}()
	}

	go func() {
		// Migration is simple: read the definition and receipt from the old cache and write it to the new cache.
		// The output files use the same directory format as the old cache.
		for _, hash := range hashes {
			hashesChannel <- hash
		}
		close(hashesChannel)
	}()

	doneChan := make(chan struct{})

	go func() {
		wg.Wait()
		close(doneChan)
	}()

	select {
	case <-doneChan:
		return nil
	case err := <-errorChannel:
		return err
	}
}

var (
	repo             = flag.String("repo", "https://packages.wolfi.dev/os", "Alpine repository to use")
	buildDir         = flag.String("build-dir", "local/apkRepo", "Directory to store build files")
	arch             = flag.String("arch", "x86_64", "Architecture to build for")
	indexOutput      = flag.String("index-output", "local/apkRepo/index", "Directory to store index files")
	contentsOutput   = flag.String("contents-output", "local/apkRepo/contents", "Directory to store contents files")
	jobs             = flag.Int("jobs", 1, "Number of jobs to run in parallel")
	maxThreads       = flag.Int("max-threads", 10000, "Set the maximum number of threads")
	migrateToSplit   = flag.Bool("migrate-to-split", false, "Migrate to the split build cache")
	migrationThreads = flag.Int("migration-threads", 32, "Number of threads to use for migration")
	useSplit         = flag.Bool("use-split", false, "Use the split build cache")
	splitDefinitions = flag.String("split-definitions", "local/apkRepo/build/definitions", "Directory to store split definitions")
	splitReceipts    = flag.String("split-receipts", "local/apkRepo/build/receipts", "Directory to store split receipts")
	pprofAddr        = flag.String("pprof-addr", "", "Address to serve pprof on")
)

func appMain() error {
	if err := common.EnableVerbose(); err != nil {
		return err
	}

	flag.Parse()

	if *pprofAddr != "" {
		go func() {
			slog.Info("serving pprof", "addr", *pprofAddr)
			if err := http.ListenAndServe(*pprofAddr, nil); err != nil {
				slog.Error("failed to serve pprof", "error", err)
			}
		}()
	}

	if *migrateToSplit {
		return migrateToSplitBuildCache(
			*buildDir,
			*splitDefinitions,
			*splitReceipts,
			*migrationThreads,
		)
	}

	debug.SetMaxThreads(*maxThreads)
	// feature.ToggleFeature(feature.FeatureTokenLockerDebug)

	db, err := database.New(func(pd common.PackageDatabase) (common.Builder, error) {
		if err := common.Ensure(*buildDir, os.ModePerm); err != nil {
			return nil, err
		}

		logger := build2.NewSimpleLogger()

		var buildFs build2.BuildCacheFilesystem

		if *useSplit {
			if err := common.Ensure(*splitDefinitions, os.ModePerm); err != nil {
				return nil, err
			}
			defDir := filesystem.NewLocalMutableDirectory(*splitDefinitions)
			if err := common.Ensure(*splitReceipts, os.ModePerm); err != nil {
				return nil, err
			}
			receiptDir := filesystem.NewLocalMutableDirectory(*splitReceipts)
			outputDir := filesystem.NewLocalMutableDirectory(*buildDir)

			buildFs = newSplitBuildDirectory(defDir, receiptDir, outputDir)
		} else {
			mutBuildDir := filesystem.NewLocalMutableDirectory(*buildDir)

			buildFs = build2.NewFilesystemBuildCache(mutBuildDir)
		}

		return build2.New(buildFs, pd, *jobs, logger.Group("root")), nil
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

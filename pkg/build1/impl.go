package build1

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/schollz/progressbar/v3"
	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/config"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/hash"
	"github.com/tinyrange/tinyrange/pkg/star"
	"go.starlark.net/starlark"
)

type buildStatusKind byte

const (
	buildStatusBuilt buildStatusKind = iota
	buildStatusCached
)

func (s buildStatusKind) String() string {
	switch s {
	case buildStatusBuilt:
		return "Built"
	case buildStatusCached:
		return "Cached"
	default:
		return "<unknown BuildStatusKind>"
	}
}

type buildStatus struct {
	Status   buildStatusKind
	Children []common.BuildDefinition
}

func runVMM(exe string, buildDir string, configFilename string) (*exec.Cmd, error) {
	persistPath := filepath.Join(buildDir, "persist")

	if err := common.Ensure(persistPath, os.ModePerm); err != nil {
		return nil, err
	}

	cmd := exec.Command(exe, "-build-dir", buildDir, "-persist-path", persistPath, configFilename)

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	slog.Debug("executing VMM", "args", cmd.Args)

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	return cmd, nil
}

type buildContext struct {
	def      common.BuildDefinition
	hash     hash.Hash
	builder  *builder1
	parent   *buildContext
	status   *buildStatus
	children []*buildContext

	filename  string
	output    io.WriteCloser
	lastBuild time.Time
}

// CreateFile implements common.BuildContext.
func (b *buildContext) CreateFile(name string) (io.WriteCloser, error) {
	return nil, fmt.Errorf("not implemented")
}

// Describe implements common.BuildContext.
func (b *buildContext) Describe(format string, args ...interface{}) {
	slog.Info("describe", "hash", b.hash, "message", fmt.Sprintf(format, args...))
}

// Logf implements common.BuildContext.
func (b *buildContext) Logf(format string, args ...interface{}) {
	slog.Info("log", "hash", b.hash, "message", fmt.Sprintf(format, args...))
}

// WriteDefault implements common.BuildContext.
func (b *buildContext) WriteDefault(result common.BuildResult) error {
	defFile, err := b.CreateDefault()
	if err != nil {
		return fmt.Errorf("could not create default file for %T: %w", b.def, err)
	}

	return result.WriteResult(defFile)
}

// DefinitionHash implements common.BuildContext.
func (b *buildContext) DefinitionHash() hash.Hash {
	return b.hash
}

// ShouldRebuildUserDefinitions implements common.BuildContext.
func (b *buildContext) ShouldRebuildUserDefinitions() bool {
	return b.builder.rebuildUserDefinitions
}

// BuildDir implements common.BuildContext.
func (b *buildContext) BuildDir() string {
	return b.builder.buildDir
}

func (b *buildContext) RunVMM(name string, vmCfg config.TinyRangeConfig) (*exec.Cmd, error) {
	configFilename, out, err := b.createFile(".json")
	if err != nil {
		return nil, err
	}

	enc := json.NewEncoder(out)

	if err := enc.Encode(&vmCfg); err != nil {
		out.Close()
		return nil, err
	}

	if err := out.Close(); err != nil {
		return nil, err
	}

	if name == "" {
		return nil, common.ErrTemplateBuilt(configFilename)
	}

	var exe string

	if name == "qemu" {
		exe, err = common.GetAdjacentExecutable("tinyrange_qemu", "tinyqemu/tinyrange_qemu")
		if err != nil {
			return nil, err
		}
	} else {
		return nil, fmt.Errorf("unknown VMM: %s", name)
	}

	return runVMM(exe, b.BuildDir(), configFilename)
}

// LsatBuild implements common.BuildContext.
func (b *buildContext) LastBuild() time.Time {
	return b.lastBuild
}

// createFile implements common.BuildContext.
func (b *buildContext) createFile(name string) (string, io.WriteCloser, error) {
	out, err := os.Create(b.filename + name)
	if err != nil {
		return "", nil, err
	}

	return out.Name(), out, nil
}

// DigestFromFile implements common.BuildContext.
func (b *buildContext) DigestFromFile(file filesystem.File) (*filesystem.FileDigest, error) {
	filename, err := filesystem.GetHostFilename(file)
	if err != nil {
		return nil, err
	}

	return &filesystem.FileDigest{Hash: filename}, nil
}

// HostFilenameFromFile implements common.BuildContext.
func (b *buildContext) HostFilenameFromFile(file filesystem.File) (string, error) {
	return filesystem.GetHostFilename(file)
}

// FileFromDigest implements common.BuildContext.
func (b *buildContext) FileFromDigest(digest *filesystem.FileDigest) (filesystem.File, error) {
	if digest.Hash != "" {
		return filesystem.NewLocalFile(digest.Hash, nil), nil
	}

	return nil, fmt.Errorf("could not convert digest to hash")
}

// Database implements common.BuildContext.
func (b *buildContext) Database() common.PackageDatabase {
	return b.builder.database
}

func (b *buildContext) childContext(def common.BuildDefinition, status *buildStatus, filename string) *buildContext {
	ctx := &buildContext{
		parent:   b,
		filename: filename,
		output:   nil,
		status:   status,
		def:      def,
		builder:  b.builder,
	}

	b.children = append(b.children, ctx)

	return ctx
}

func (b *buildContext) CreateDefault() (io.WriteCloser, error) {
	if b.output != nil {
		return nil, fmt.Errorf("output already created")
	}

	out, err := os.Create(b.filename)
	if err != nil {
		return nil, err
	}

	b.output = out

	return b.output, nil
}

func (b *buildContext) HasCreatedOutput() bool {
	return b.output != nil
}

func (b *buildContext) BuildChild(def common.BuildDefinition) (common.BuildArtifact, error) {
	if b.status != nil {
		b.status.Children = append(b.status.Children, def)
	}

	return b.builder.build(b, def, common.BuildOptions{})
}

func (c *buildContext) Attr(name string) (starlark.Value, error) {
	return star.BuildContextAttr(c, name)
}

func (c *buildContext) AttrNames() []string {
	return star.BuildContextAttrNames()
}

func (*buildContext) String() string        { return "BuildContext" }
func (*buildContext) Type() string          { return "BuildContext" }
func (*buildContext) Hash() (uint32, error) { return 0, fmt.Errorf("BuildContext is not hashable") }
func (*buildContext) Truth() starlark.Bool  { return starlark.True }
func (*buildContext) Freeze()               {}

var (
	_ starlark.Value      = &buildContext{}
	_ starlark.HasAttrs   = &buildContext{}
	_ common.BuildContext = &buildContext{}
)

type tempArtifact struct {
	hash        hash.Hash
	defaultFile filesystem.File
}

// DigestFromFile implements common.BuildContext.
func (c *tempArtifact) DigestFromFile(file filesystem.File) (*filesystem.FileDigest, error) {
	filename, err := filesystem.GetHostFilename(file)
	if err != nil {
		return nil, err
	}

	return &filesystem.FileDigest{Hash: filename}, nil
}

// FileFromDigest implements common.BuildContext.
func (c *tempArtifact) FileFromDigest(digest *filesystem.FileDigest) (filesystem.File, error) {
	if digest.Hash != "" {
		return filesystem.NewLocalFile(digest.Hash, nil), nil
	}

	return nil, fmt.Errorf("could not convert digest to hash")
}

// HostFilenameFromFile implements common.BuildContext.
func (c *tempArtifact) HostFilenameFromFile(file filesystem.File) (string, error) {
	return filesystem.GetHostFilename(file)
}

// Default implements common.BuildArtifact.
func (t *tempArtifact) Default() (filesystem.File, error) {
	return t.defaultFile, nil
}

// Hash implements common.BuildArtifact.
func (t *tempArtifact) DefinitionHash() hash.Hash {
	return t.hash
}

// OpenFile implements common.BuildArtifact.
func (t *tempArtifact) OpenFile(name string) (filesystem.FileHandle, error) {
	return nil, fmt.Errorf("unimplemented")
}

// Receipt implements common.BuildArtifact.
func (t *tempArtifact) Receipt() common.BuildReceipt {
	return common.BuildReceipt{}
}

var (
	_ common.BuildArtifact = &tempArtifact{}
)

type builder1 struct {
	database common.PackageDatabase

	rebuildUserDefinitions bool

	distributionServer string

	buildCache map[hash.Hash]common.BuildArtifact

	buildStatusMtx sync.Mutex
	buildStatuses  map[common.BuildDefinition]*buildStatus

	defDb *hash.DefinitionDatabase

	buildDir string
}

// MinimalContext implements common.Builder.
func (db *builder1) MinimalContext() common.MinimalBuildContext {
	return db.newBuildContext(nil)
}

// SetRebuildUserDefinitions implements common.PackageDatabase.
func (db *builder1) SetRebuildUserDefinitions(rebuild bool) {
	db.rebuildUserDefinitions = rebuild
}

// HashDefinition implements common.PackageDatabase.
func (db *builder1) HashDefinition(def common.BuildDefinition) (hash.Hash, error) {
	return db.defDb.HashDefinition(def)
}

func (db *builder1) newBuildContext(def common.BuildDefinition) *buildContext {
	return &buildContext{def: def, builder: db}
}

func (db *builder1) updateBuildStatus(def common.BuildDefinition, status *buildStatus) {
	db.buildStatusMtx.Lock()
	defer db.buildStatusMtx.Unlock()

	db.buildStatuses[def] = status
}

func (db *builder1) filenameFromHash(hash hash.Hash, suffix string) (string, error) {
	return filepath.Join(db.buildDir, string(hash)+suffix), nil
}

func (db *builder1) downloadFromDistributionServer(hash hash.Hash, def common.BuildDefinition) (bool, error) {
	if redistributable, ok := def.(common.RedistributableDefinition); !ok || !redistributable.Redistributable() {
		return false, nil // not redistributable
	}

	client, err := db.database.HttpClient()
	if err != nil {
		return false, err
	}

	url := fmt.Sprintf("%s/result/%s", db.distributionServer, hash)

	resp, err := client.Get(url)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	} else if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("bad status %s", resp.Status)
	}

	filename, err := db.filenameFromHash(hash, ".bin")
	if err != nil {
		return false, err
	}

	tmpFilename := filename + ".tmp"

	f, err := os.Create(tmpFilename)
	if err != nil {
		return false, err
	}

	pb := progressbar.DefaultBytes(resp.ContentLength, url)
	defer pb.Close()

	if _, err := io.Copy(io.MultiWriter(f, pb), resp.Body); err != nil {
		f.Close()
		os.Remove(tmpFilename)
		return false, err
	}

	if err := f.Close(); err != nil {
		return false, err
	}

	if err := os.Rename(tmpFilename, filename); err != nil {
		return false, err
	}

	downloadedTag, err := db.filenameFromHash(hash, ".downloaded")
	if err != nil {
		return false, err
	}

	if err := os.WriteFile(downloadedTag, []byte(""), os.ModePerm); err != nil {
		return false, err
	}

	return true, nil
}

func (db *builder1) getBuildStatus(def common.BuildDefinition) (*buildStatus, error) {
	status, ok := db.buildStatuses[def]
	if !ok {
		return nil, fmt.Errorf("build status not found")
	}
	return status, nil
}

func (db *builder1) build(c common.BuildContext, def common.BuildDefinition, opts common.BuildOptions) (common.BuildArtifact, error) {
	hash, err := db.HashDefinition(def)
	if err != nil {
		return nil, err
	}

	if f, ok := db.buildCache[hash]; ok {
		return f, nil
	}

	status := &buildStatus{}

	filename, err := db.filenameFromHash(hash, ".bin")
	if err != nil {
		return nil, err
	}

	downloadedTag, err := db.filenameFromHash(hash, ".downloaded")
	if err != nil {
		return nil, err
	}

	tmpFilename := filename + ".tmp"

	ctx, ok := c.(*buildContext)
	if !ok {
		return nil, fmt.Errorf("expected buildContext, got %T", c)
	}

	ctx.hash = hash

	// Get a child context for the build.
	child := ctx.childContext(def, status, tmpFilename)

	if !opts.AlwaysRebuild {
		// Check if the file already exists. If it does then return it.
		if info, err := os.Stat(filename); err == nil {
			var needsRebuild = false

			child.lastBuild = info.ModTime()

			// Only check for rebuilds if the child is not downloaded.
			if exists, _ := common.Exists(downloadedTag); !exists {
				// If the file has already been created then check if a rebuild is needed.
				needsRebuild, err = def.NeedsBuild(child)
				if err != nil {
					return nil, err
				}
			} else {
				// Redistributed results are considered user definitions.
				if db.rebuildUserDefinitions {
					needsRebuild = true
				}
			}

			// If no rebuild is necessary then skip it.
			if !needsRebuild {
				status.Status = buildStatusCached

				// Write the build status.
				db.updateBuildStatus(def, status)

				slog.Debug("cached", "Hash", hash.String(), "filename", filename)

				return &tempArtifact{
					hash:        hash,
					defaultFile: filesystem.NewLocalFile(filename, def),
				}, nil
			}

			slog.Debug("rebuild requested", "Hash", hash.String())
		} else {
			slog.Debug("building", "Hash", hash.String())
		}
	} else {
		slog.Debug("building", "Hash", hash.String())
	}

	defValue, err := db.defDb.MarshalDefinition(def)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal definition: %s", err)
	}

	defFilename, err := db.filenameFromHash(hash, ".def")
	if err != nil {
		return nil, err
	}

	if err := os.WriteFile(defFilename, defValue, os.ModePerm); err != nil {
		return nil, fmt.Errorf("failed to write definition: %s", err)
	}

	if db.distributionServer != "" {
		// If we have a distribution server then check it first.
		ok, err := db.downloadFromDistributionServer(hash, def)
		if err != nil {
			return nil, err
		}

		if ok {
			status.Status = buildStatusBuilt

			db.updateBuildStatus(def, status)

			// This definition is redistributable so write a manifest.
			redistributableTag, err := db.filenameFromHash(hash, ".redistributable")
			if err != nil {
				return nil, err
			}

			if err := os.WriteFile(redistributableTag, []byte(""), os.ModePerm); err != nil {
				return nil, err
			}

			art := &tempArtifact{
				hash:        hash,
				defaultFile: filesystem.NewLocalFile(filename, def),
			}

			db.buildCache[hash] = art

			// Return the file.
			return art, nil
		}
	}

	// If the downloaded tag exists then remove it.

	// If not then trigger the build.
	err = def.Build(child)
	if err == common.ErrUseExistingBuild {
		status.Status = buildStatusCached

		// Write the build status.
		db.updateBuildStatus(def, status)

		return &tempArtifact{
			hash:        hash,
			defaultFile: filesystem.NewLocalFile(filename, def),
		}, nil
	} else if err != nil {
		return nil, err
	}

	// If the build has already been written then don't write it again.
	if !child.HasCreatedOutput() {
		return nil, fmt.Errorf("output not created")
	} else {
		// Close the output file.
		// Ignore errors since it may have already been closed.
		child.output.Close()
	}

	// Finally rename the temporary file to the final filename.
	if err := os.Rename(tmpFilename, filename); err != nil {
		os.Remove(tmpFilename)
		return nil, err
	}

	status.Status = buildStatusBuilt

	// Write the build status.
	db.updateBuildStatus(def, status)

	if redistributable, ok := def.(common.RedistributableDefinition); ok && redistributable.Redistributable() {
		// This definition is redistributable so write a manifest.

		redistributableTag, err := db.filenameFromHash(hash, ".redistributable")
		if err != nil {
			return nil, err
		}

		if err := os.WriteFile(redistributableTag, []byte(""), os.ModePerm); err != nil {
			return nil, err
		}
	}

	art := &tempArtifact{
		hash:        hash,
		defaultFile: filesystem.NewLocalFile(filename, def),
	}

	db.buildCache[hash] = art

	// Return the file.
	return art, nil
}

func (db *builder1) Build(def common.BuildDefinition, opts common.BuildOptions) (common.BuildArtifact, error) {
	return db.build(db.newBuildContext(def), def, opts)
}

func (db *builder1) missDefinitionCache(hash hash.Hash) (io.ReadCloser, error) {
	filename, err := db.filenameFromHash(hash, ".def")
	if err != nil {
		return nil, err
	}

	return os.Open(filename)
}

func (db *builder1) GetDefinitionByHash(hash hash.Hash) (common.BuildDefinition, error) {
	def, err := db.defDb.GetDefinitionByHash(hash)
	if err != nil {
		return nil, err
	}

	return def.(common.BuildDefinition), nil
}

func (db *builder1) SetDistributionServer(server string) error {
	client, err := db.database.HttpClient()
	if err != nil {
		return err
	}

	resp, err := client.Get(server + "/health")
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if !slices.Equal(content, []byte("OK")) {
		return fmt.Errorf("bad response from distribution server")
	}

	db.distributionServer = server

	return nil
}

// GarbageCollect implements common.Builder.
func (db *builder1) GarbageCollect(olderThan time.Time) ([]hash.Hash, error) {
	return nil, fmt.Errorf("unimplemented")
}

var (
	_ common.Builder = &builder1{}
)

func NewBuilder(buildDir string) common.BuilderFactor {
	return func(db common.PackageDatabase) (common.Builder, error) {
		builder := &builder1{
			database:      db,
			buildStatuses: make(map[common.BuildDefinition]*buildStatus),
			buildCache:    make(map[hash.Hash]common.BuildArtifact),
			buildDir:      buildDir,
		}

		builder.defDb = hash.NewDefinitionDatabase(builder.missDefinitionCache)

		// Check with Exists first so it doesn't have issues if the build dir is behind a symlink.
		if ok, _ := common.Exists(buildDir); !ok {
			if err := common.Ensure(buildDir, os.ModePerm); err != nil {
				return nil, err
			}
		}

		return builder, nil
	}
}

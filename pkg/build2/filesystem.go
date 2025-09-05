package build2

import (
	"encoding/json"
	"errors"
	"fmt"
	"hash/adler32"
	"io"
	"io/fs"
	"net/http"
	"regexp"
	"time"

	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/config"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/filesystem/dbconfig"
	"github.com/tinyrange/tinyrange/pkg/hash"
	"github.com/tinyrange/tinyrange/pkg/log"
)

const (
	definitionFileName = "definition.bin"
	receiptFileName    = "receipt.json"
	outputPrefix       = "output."

	MARKER_FILENAME = "tinyrange-build.json"
	MARKER_VERSION  = 1
)

type MarkerHeader struct {
	Version int `json:"version"`
}

func checkMarkerFile(dir filesystem.Directory) error {
	// check for the presence of a marker file.
	marker, err := dir.GetChild(MARKER_FILENAME)
	if err != nil {
		return fmt.Errorf("failed to get marker file: %w", err)
	}

	// Open the marker and check the version
	markerFile, err := marker.Open()
	if err != nil {
		return fmt.Errorf("failed to open marker file: %w", err)
	}
	defer markerFile.Close()

	var markerHeader MarkerHeader
	if err := json.NewDecoder(markerFile).Decode(&markerHeader); err != nil {
		return fmt.Errorf("failed to decode marker file: %w", err)
	}

	if markerHeader.Version > MARKER_VERSION {
		return fmt.Errorf("marker file is newer than the current version: %d > %d", markerHeader.Version, MARKER_VERSION)
	}

	return nil
}

type filesystemOutputFileHandle struct {
	filesystem.WritableFileHandle

	mut filesystem.MutableFile
}

// GetHostFilename implements OutputFileHandle.
func (f *filesystemOutputFileHandle) GetHostFilename() (string, error) {
	return filesystem.GetHostFilename(f.mut)
}

var (
	_ common.OutputFileHandle = &filesystemOutputFileHandle{}
)

type readOnlyFilesystemBuildDirectory struct {
	dir filesystem.Directory
}

// ReadDefinition implements BuildCacheDirectory.
func (f *readOnlyFilesystemBuildDirectory) ReadDefinition() ([]byte, error) {
	dh, err := f.dir.GetChild(definitionFileName)
	if err != nil {
		// This error could not not-exist so we propagate it.
		return nil, err
	}

	file, err := dh.Open()
	if err != nil {
		return nil, err
	}
	defer file.Close()

	def, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}

	return def, nil
}

// File implements BuildCacheDirectory.
func (f *readOnlyFilesystemBuildDirectory) File(name string) (filesystem.File, error) {
	file, err := f.dir.GetChild(outputPrefix + name)
	if err != nil {
		return nil, err
	}

	return file.File, nil
}

// ReadReceipt implements BuildCacheDirectory.
func (f *readOnlyFilesystemBuildDirectory) ReadReceipt() ([]byte, error) {
	dh, err := f.dir.GetChild(receiptFileName)
	if err != nil {
		// This error could not not-exist so we propagate it.
		return nil, err
	}

	file, err := dh.Open()
	if err != nil {
		return nil, err
	}
	defer file.Close()

	recept, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}

	return recept, nil
}

var (
	_ common.BuildCacheDirectory = &readOnlyFilesystemBuildDirectory{}
)

type readOnlyBuildCache struct {
	dir    filesystem.Directory
	config dbconfig.BuildDatabaseConfig
}

// Config implements ReadOnlyBuildCache.
func (f *readOnlyBuildCache) Config() dbconfig.BuildDatabaseConfig {
	return f.config
}

func (f *readOnlyBuildCache) GetBuildDirectoryFromCache(hash hash.Hash) (common.BuildCacheDirectory, error) {
	buildEntTop, err := f.dir.GetChild(hash.String()[:2])
	if err != nil {
		return nil, fmt.Errorf("failed to get cache build directory: %w", err)
	}

	buildDirTop, ok := buildEntTop.File.(filesystem.Directory)
	if !ok {
		return nil, fmt.Errorf("cache build directory is not a directory")
	}

	buildEnt, err := buildDirTop.GetChild(hash.String()[2:])
	if err != nil {
		return nil, fmt.Errorf("failed to get build directory: %w", err)
	}

	buildDir, ok := buildEnt.File.(filesystem.Directory)
	if !ok {
		return nil, fmt.Errorf("build directory is not a directory")
	}

	return &readOnlyFilesystemBuildDirectory{dir: buildDir}, nil
}

var (
	_ ReadOnlyBuildCache = &readOnlyBuildCache{}
)

type filesystemBuildDirectory struct {
	*readOnlyFilesystemBuildDirectory

	dir filesystem.MutableDirectory
}

// CreateOutputFile implements BuildCacheDirectory.
func (f *filesystemBuildDirectory) CreateOutputFile(name string) (common.OutputFileHandle, error) {
	file, err := f.dir.Create(outputPrefix+name, nil)
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

// WriteDefinition implements BuildCacheDirectory.
func (f *filesystemBuildDirectory) WriteDefinition(def []byte) error {
	memFile := filesystem.Factory.NewMemoryFile()
	if err := memFile.Overwrite(def); err != nil {
		return err
	}

	if _, err := f.dir.Create(definitionFileName, memFile); err != nil {
		return err
	}

	return nil
}

// WriteReceipt implements BuildCacheDirectory.
func (f *filesystemBuildDirectory) WriteReceipt(recept []byte) error {
	memFile := filesystem.Factory.NewMemoryFile()
	if err := memFile.Overwrite(recept); err != nil {
		return err
	}

	if _, err := f.dir.Create(receiptFileName, memFile); err != nil {
		return err
	}

	return nil
}

var (
	_ common.WritableBuildCacheDirectory = &filesystemBuildDirectory{}
)

var DEFAULT_DATABASE_CONFIG = dbconfig.BuildDatabaseConfig{
	RelativeHostBuildDirectory: &dbconfig.RelativeHostBuildDirectory{
		RelativePath: "../..",
	},
}

type BuildCacheFilesystem interface {
	common.BuildCacheFilesystem

	AddCacheDirectory(dir filesystem.Directory, config dbconfig.BuildDatabaseConfig) error
	AddRemoteCacheDirectory(baseUrl string, config dbconfig.BuildDatabaseConfig) error
}

type remoteBuildOutput struct {
	name string
	hash string
	size int64
	dir  *remoteBuildDirectory
}

func (r *remoteBuildOutput) Id() uint64 {
	return uint64(adler32.Checksum([]byte(fmt.Sprintf("%s/%s", r.dir.hash.String(), r.name))))
}
func (r *remoteBuildOutput) IsDir() bool               { return false }
func (r *remoteBuildOutput) Kind() filesystem.FileType { return filesystem.TypeRegular }
func (r *remoteBuildOutput) ModTime() time.Time {
	return r.dir.receipt.StartTime.Add(r.dir.receipt.Duration)
}
func (r *remoteBuildOutput) Mode() fs.FileMode { return fs.ModePerm }
func (r *remoteBuildOutput) Name() string      { return outputPrefix + r.name }
func (r *remoteBuildOutput) Size() int64       { return r.size }
func (r *remoteBuildOutput) Sys() any          { return nil }

// Open implements filesystem.File.
func (r *remoteBuildOutput) Open() (filesystem.FileHandle, error) {
	readerAt, err := r.dir.cache.cache.GetOrSet(r.hash, func(w io.Writer) error {
		fileUrl := fmt.Sprintf("%s/%s/%s/%s", r.dir.cache.baseUrl, r.dir.hash.String()[:2], r.dir.hash.String()[2:], outputPrefix+r.name)
		resp, err := r.dir.cache.client.Get(fileUrl)
		if err != nil {
			return fmt.Errorf("failed to get file: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("failed to get file: %s", resp.Status)
		}

		if _, err := io.Copy(w, resp.Body); err != nil {
			return fmt.Errorf("failed to copy file: %w", err)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get file: %w", err)
	}

	return filesystem.NewSimpleFileHandle(readerAt, r.size), nil
}

// Stat implements filesystem.File.
func (r *remoteBuildOutput) Stat() (filesystem.FileInfo, error) {
	return r, nil
}

var (
	_ filesystem.File = &remoteBuildOutput{}
)

type remoteBuildDirectory struct {
	cache      *remoteBuildCache
	hash       hash.Hash
	receipt    common.BuildReceipt
	definition []byte
}

// File implements common.BuildCacheDirectory.
func (r *remoteBuildDirectory) File(name string) (filesystem.File, error) {
	if _, ok := r.receipt.Files[name]; !ok {
		return nil, fmt.Errorf("file %s not found in receipt", name)
	}

	// send a head request to ensure the file exists and get the size
	fileUrl := fmt.Sprintf("%s/%s/%s/%s", r.cache.baseUrl, r.hash.String()[:2], r.hash.String()[2:], outputPrefix+name)
	resp, err := r.cache.client.Head(fileUrl)
	if err != nil {
		return nil, fmt.Errorf("failed to get file size: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get file size: %s", resp.Status)
	}
	if err := resp.Body.Close(); err != nil {
		return nil, fmt.Errorf("failed to close response body: %w", err)
	}

	return &remoteBuildOutput{
		name: name,
		hash: r.receipt.Files[name],
		size: resp.ContentLength,
		dir:  r,
	}, nil
}

// ReadDefinition implements common.BuildCacheDirectory.
func (r *remoteBuildDirectory) ReadDefinition() ([]byte, error) {
	if r.definition != nil {
		return r.definition, nil
	}

	// try to request the definition file
	definitionUrl := fmt.Sprintf("%s/%s/%s/%s", r.cache.baseUrl, r.hash.String()[:2], r.hash.String()[2:], definitionFileName)

	resp, err := r.cache.client.Get(definitionUrl)
	if err != nil {
		return nil, fmt.Errorf("failed to get definition file: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get definition file: %s", resp.Status)
	}
	defer resp.Body.Close()

	definition, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read definition file: %w", err)
	}

	r.definition = definition

	return definition, nil
}

// ReadReceipt implements common.BuildCacheDirectory.
func (r *remoteBuildDirectory) ReadReceipt() ([]byte, error) {
	// encode the local receipt
	receipt, err := json.Marshal(r.receipt)
	if err != nil {
		return nil, fmt.Errorf("failed to encode receipt: %w", err)
	}

	return receipt, nil
}

var (
	_ common.BuildCacheDirectory = &remoteBuildDirectory{}
)

type remoteBuildCache struct {
	cache   common.SimpleCache
	client  *http.Client
	baseUrl string
	config  dbconfig.BuildDatabaseConfig
}

// Config implements ReadOnlyBuildCache.
func (r *remoteBuildCache) Config() dbconfig.BuildDatabaseConfig {
	return r.config
}

// GetBuildDirectoryFromCache implements ReadOnlyBuildCache.
func (r *remoteBuildCache) GetBuildDirectoryFromCache(hash hash.Hash) (common.BuildCacheDirectory, error) {
	// try to request the receipt file
	receiptUrl := fmt.Sprintf("%s/%s/%s/%s", r.baseUrl, hash.String()[:2], hash.String()[2:], receiptFileName)

	resp, err := r.client.Get(receiptUrl)
	if err != nil {
		return nil, fmt.Errorf("failed to get receipt file: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get receipt file: %s", resp.Status)
	}
	defer resp.Body.Close()

	var receipt common.BuildReceipt
	if err := json.NewDecoder(resp.Body).Decode(&receipt); err != nil {
		return nil, fmt.Errorf("failed to decode receipt file: %w", err)
	}

	return &remoteBuildDirectory{
		cache:   r,
		hash:    hash,
		receipt: receipt,
	}, nil
}

var (
	_ ReadOnlyBuildCache = &remoteBuildCache{}
)

type ReadOnlyBuildCache interface {
	GetBuildDirectoryFromCache(hash hash.Hash) (common.BuildCacheDirectory, error)

	Config() dbconfig.BuildDatabaseConfig
}

type filesystemBuildCache struct {
	baseConfig dbconfig.BuildDatabaseConfig
	dir        filesystem.MutableDirectory
	log        log.Handler

	cacheDirectories []ReadOnlyBuildCache
}

// Append implements common.SimpleCache.
func (f *filesystemBuildCache) Append(key string) (io.WriteCloser, error) {
	cacheDir, err := f.dir.Mkdir("cache")
	if err != nil {
		return nil, fmt.Errorf("failed to create cache directory: %w", err)
	}

	ent, err := cacheDir.GetChild(key)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("failed to get cache entry: %w", err)
		}

		// create the cache entry
		cacheFile, err := cacheDir.Create(key, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create cache entry: %w", err)
		}

		mut, ok := cacheFile.(filesystem.MutableAppendFile)
		if !ok {
			return nil, fmt.Errorf("cache entry is not appendable: %T", cacheFile)
		}

		return mut.OpenMutAppend()
	}

	mut, ok := ent.File.(filesystem.MutableAppendFile)
	if !ok {
		return nil, fmt.Errorf("cache entry is not appendable: %T", ent.File)
	}

	return mut.OpenMutAppend()
}

// Open implements common.SimpleCache.
func (f *filesystemBuildCache) Open(key string) (io.ReadCloser, error) {
	cacheDir, err := f.dir.Mkdir("cache")
	if err != nil {
		return nil, fmt.Errorf("failed to create cache directory: %w", err)
	}

	ent, err := cacheDir.GetChild(key)
	if err != nil {
		return nil, fmt.Errorf("failed to get cache entry: %w", err)
	}

	return ent.Open()
}

func (f *filesystemBuildCache) HttpClient() *http.Client {
	return http.DefaultClient
}

// GetOrSet implements common.SimpleCache.
func (f *filesystemBuildCache) GetOrSet(hash string, setter func(w io.Writer) error) (io.ReaderAt, error) {
	cacheDir, err := f.dir.Mkdir("cache")
	if err != nil {
		return nil, fmt.Errorf("failed to create cache directory: %w", err)
	}

	ent, err := cacheDir.GetChild(hash)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("failed to get cache entry: %w", err)
		}

		// create the cache entry
		cacheFile, err := cacheDir.Create(hash, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create cache entry: %w", err)
		}

		cacheFileMut, ok := cacheFile.(filesystem.MutableFile)
		if !ok {
			return nil, fmt.Errorf("cache entry is not mutable: %T", cacheFile)
		}

		handle, err := cacheFileMut.OpenMut()
		if err != nil {
			return nil, fmt.Errorf("failed to open cache entry: %w", err)
		}

		if err := setter(handle); err != nil {
			handle.Close()
			return nil, fmt.Errorf("failed to set cache entry: %w", err)
		}

		if err := handle.Close(); err != nil {
			return nil, fmt.Errorf("failed to close cache entry: %w", err)
		}

		ent, err = cacheDir.GetChild(hash)
		if err != nil {
			return nil, fmt.Errorf("failed to get cache entry: %w", err)
		}
	}

	handle, err := ent.Open()
	if err != nil {
		return nil, fmt.Errorf("failed to open cache entry: %w", err)
	}

	return handle, nil
}

// SimpleCache implements BuildCacheFilesystem.
func (f *filesystemBuildCache) SimpleCache() common.SimpleCache {
	return f
}

// AddCacheDirectory implements BuildCacheFilesystem.
func (f *filesystemBuildCache) AddCacheDirectory(dir filesystem.Directory, config dbconfig.BuildDatabaseConfig) error {
	if dir == nil {
		return fmt.Errorf("directory is nil")
	}

	if _, err := dir.Stat(); err != nil {
		return fmt.Errorf("failed to access cache directory: %w", err)
	}

	if err := checkMarkerFile(dir); err != nil {
		return fmt.Errorf("failed to check marker file in cache directory: %w", err)
	}

	f.cacheDirectories = append(f.cacheDirectories, &readOnlyBuildCache{
		dir:    dir,
		config: config,
	})

	return nil
}

// AddRemoteCacheDirectory implements BuildCacheFilesystem.
func (f *filesystemBuildCache) AddRemoteCacheDirectory(baseUrl string, config dbconfig.BuildDatabaseConfig) error {
	markerUrl := fmt.Sprintf("%s/%s", baseUrl, MARKER_FILENAME)

	// try to get the marker file from the remote cache
	markerResp, err := f.HttpClient().Get(markerUrl)
	if err != nil {
		return fmt.Errorf("failed to get marker file from remote cache: %w", err)
	}

	if markerResp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to get marker file from remote cache: %s %s", markerUrl, markerResp.Status)
	}
	defer markerResp.Body.Close()

	// check the version of the marker file
	var markerHeader MarkerHeader
	if err := json.NewDecoder(markerResp.Body).Decode(&markerHeader); err != nil {
		return fmt.Errorf("failed to decode marker file from remote cache: %w", err)
	}
	if markerHeader.Version > MARKER_VERSION {
		return fmt.Errorf("marker file from remote cache is newer than the current version: %d > %d", markerHeader.Version, MARKER_VERSION)
	}

	f.cacheDirectories = append(f.cacheDirectories, &remoteBuildCache{
		cache:   f.SimpleCache(),
		client:  f.HttpClient(),
		baseUrl: baseUrl,
		config:  config,
	})

	return nil
}

// FileFromReference implements common.BuildCacheFilesystem.
func (f *filesystemBuildCache) FileFromReference(ref config.DatabaseReference) (filesystem.File, error) {
	buildDir, err := f.GetBuildDirectory(hash.Hash(ref.Hash))
	if err != nil {
		return nil, fmt.Errorf("failed to get build directory: %w", err)
	}

	file, err := buildDir.File(ref.Filename)
	if err != nil {
		return nil, fmt.Errorf("failed to get output file: %w", err)
	}

	return file, nil
}

// DatabaseConfig implements common.BuildCacheFilesystem.
func (f *filesystemBuildCache) DatabaseConfig() ([]dbconfig.BuildDatabaseConfig, error) {
	var ret []dbconfig.BuildDatabaseConfig

	// add the base config
	ret = append(ret, f.baseConfig)

	for _, config := range f.cacheDirectories {
		ret = append(ret, config.Config())
	}

	if len(ret) == 0 {
		return nil, fmt.Errorf("no cache directories")
	}

	return ret, nil
}

// GetHostFilename implements BuildCacheFilesystem.
func (f *filesystemBuildCache) GetHostFilename() (string, error) {
	return filesystem.GetHostFilename(f.dir)
}

// CreateOrGetBuildDirectory implements BuildCacheFilesystem.
func (f *filesystemBuildCache) CreateBuildDirectory(hash hash.Hash) (common.BuildCacheDirectory, error) {
	// take the first byte of the hash as the directory name
	buildDirTop, err := f.dir.Mkdir(hash.String()[:2])
	if err != nil {
		return nil, fmt.Errorf("failed to create top build directory: %w", err)
	}

	// create a new build directory
	// If it already exists, it will be reused.
	buildDir, err := buildDirTop.Mkdir(hash.String()[2:])
	if err != nil {
		return nil, fmt.Errorf("failed to create build directory: %w", err)
	}

	return &filesystemBuildDirectory{
		readOnlyFilesystemBuildDirectory: &readOnlyFilesystemBuildDirectory{
			dir: buildDir,
		},
		dir: buildDir,
	}, nil
}

func (f *filesystemBuildCache) getBuildDirectory(hash hash.Hash) (common.BuildCacheDirectory, error) {
	buildEntTop, err := f.dir.GetChild(hash.String()[:2])
	if err != nil {
		return nil, fmt.Errorf("failed to get top build directory: %w", err)
	}

	buildDirTop, ok := buildEntTop.File.(filesystem.MutableDirectory)
	if !ok {
		return nil, fmt.Errorf("top build directory is not a mutable directory")
	}

	buildEnt, err := buildDirTop.GetChild(hash.String()[2:])
	if err != nil {
		return nil, fmt.Errorf("failed to get build directory: %w", err)
	}

	buildDir, ok := buildEnt.File.(filesystem.MutableDirectory)
	if !ok {
		return nil, fmt.Errorf("build directory is not a mutable directory")
	}

	return &filesystemBuildDirectory{
		readOnlyFilesystemBuildDirectory: &readOnlyFilesystemBuildDirectory{
			dir: buildDir,
		},
		dir: buildDir,
	}, nil
}

// GetBuildDirectory implements BuildCacheFilesystem.
func (f *filesystemBuildCache) GetBuildDirectory(hash hash.Hash) (common.BuildCacheDirectory, error) {
	// try the top directory first
	top, topErr := f.getBuildDirectory(hash)
	if topErr == nil {
		return top, nil
	} else if errors.Is(topErr, fs.ErrNotExist) {
		// try cache directories
		for _, cacheDir := range f.cacheDirectories {
			buildDir, err := cacheDir.GetBuildDirectoryFromCache(hash)
			if err == nil {
				return buildDir, nil
			} else if !errors.Is(err, fs.ErrNotExist) {
				return nil, fmt.Errorf("failed to get build directory from cache: %w", err)
			}
		}
	}

	return nil, topErr
}

var (
	validByte   = regexp.MustCompile(`^[0-9a-f]{2}$`)
	validSha256 = regexp.MustCompile(`^[0-9a-f]{62}$`)
)

// GetAllHashes implements BuildCacheFilesystem.
func (f *filesystemBuildCache) GetAllHashes() ([]hash.Hash, error) {
	ents, err := f.dir.Readdir()
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	var ret []hash.Hash

	// Populate the receipts map.
	for _, ent := range ents {
		if !validByte.MatchString(ent.Name) {
			continue
		}

		childDir, ok := ent.File.(filesystem.Directory)
		if !ok {
			continue
		}

		childEnts, err := childDir.Readdir()
		if err != nil {
			f.log.Warn("failed to read directory", "err", err)
			continue
		}

		for _, childEnt := range childEnts {
			if !validSha256.MatchString(childEnt.Name) {
				continue
			}

			ret = append(ret, hash.Hash(ent.Name+childEnt.Name))
		}
	}

	return ret, nil
}

var (
	_ BuildCacheFilesystem = &filesystemBuildCache{}
)

func OpenFilesystemBuildCache(dir filesystem.MutableDirectory, config dbconfig.BuildDatabaseConfig, log log.Handler) (BuildCacheFilesystem, error) {
	ret := &filesystemBuildCache{
		dir:        dir,
		baseConfig: config,
		log:        log,
	}

	if err := checkMarkerFile(dir); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("failed to check marker file: %w", err)
	} else if err == nil {
		// marker file exists and is valid
		return ret, nil
	}

	// Create the marker file
	markerFile := filesystem.Factory.NewMemoryFile()
	markerFileHandle, err := markerFile.OpenMut()
	if err != nil {
		return nil, fmt.Errorf("failed to open marker file: %w", err)
	}

	markerHeader := MarkerHeader{
		Version: MARKER_VERSION,
	}

	if err := json.NewEncoder(markerFileHandle).Encode(markerHeader); err != nil {
		markerFileHandle.Close()
		return nil, fmt.Errorf("failed to encode marker file: %w", err)
	}

	markerFileHandle.Close()

	if _, err := dir.Create(MARKER_FILENAME, markerFile); err != nil {
		return nil, fmt.Errorf("failed to create marker file: %w", err)
	}

	return ret, nil
}

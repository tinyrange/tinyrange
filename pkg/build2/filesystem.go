package build2

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"regexp"

	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/config"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/hash"
	"github.com/tinyrange/tinyrange/pkg/log"
)

const (
	definitionFileName = "definition.json"
	receiptFileName    = "receipt.json"
	outputPrefix       = "output."

	MARKER_FILENAME = "tinyrange-build.json"
	MARKET_VERSION  = 1
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

	if markerHeader.Version > MARKET_VERSION {
		return fmt.Errorf("marker file is newer than the current version: %d > %d", markerHeader.Version, MARKET_VERSION)
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

	file, err := dh.File.Open()
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
	memFile := filesystem.NewMemoryFile(filesystem.TypeRegular)
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
	memFile := filesystem.NewMemoryFile(filesystem.TypeRegular)
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

var DEFAULT_DATABASE_CONFIG = filesystem.BuildDatabaseConfig{
	RelativeHostBuildDirectory: &filesystem.RelativeHostBuildDirectory{
		RelativePath: "../..",
	},
}

type BuildCacheFilesystem interface {
	common.BuildCacheFilesystem

	AddCacheDirectory(dir filesystem.Directory, config filesystem.BuildDatabaseConfig) error
}

type filesystemBuildCache struct {
	dir filesystem.MutableDirectory

	cacheDirectories []filesystem.Directory
	config           []filesystem.BuildDatabaseConfig
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
func (f *filesystemBuildCache) AddCacheDirectory(dir filesystem.Directory, config filesystem.BuildDatabaseConfig) error {
	if dir == nil {
		return fmt.Errorf("directory is nil")
	}

	if _, err := dir.Stat(); err != nil {
		return fmt.Errorf("failed to access cache directory: %w", err)
	}

	if err := checkMarkerFile(dir); err != nil {
		return fmt.Errorf("failed to check marker file in cache directory: %w", err)
	}

	f.cacheDirectories = append(f.cacheDirectories, dir)
	f.config = append(f.config, config)

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
func (f *filesystemBuildCache) DatabaseConfig() ([]filesystem.BuildDatabaseConfig, error) {
	if f.config == nil {
		return nil, fmt.Errorf("no database config")
	}

	return f.config, nil
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

func (f *filesystemBuildCache) getBuildDirectoryFromCache(top filesystem.Directory, hash hash.Hash) (common.BuildCacheDirectory, error) {
	buildEntTop, err := top.GetChild(hash.String()[:2])
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
			buildDir, err := f.getBuildDirectoryFromCache(cacheDir, hash)
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
			log.Warn("failed to read directory", "err", err)
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

func OpenFilesystemBuildCache(dir filesystem.MutableDirectory, config filesystem.BuildDatabaseConfig) (BuildCacheFilesystem, error) {
	ret := &filesystemBuildCache{
		dir:    dir,
		config: []filesystem.BuildDatabaseConfig{config},
	}

	if err := checkMarkerFile(dir); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("failed to check marker file: %w", err)
	}

	// Create the marker file
	markerFile := filesystem.NewMemoryFile(filesystem.TypeRegular)
	markerFileHandle, err := markerFile.OpenMut()
	if err != nil {
		return nil, fmt.Errorf("failed to open marker file: %w", err)
	}

	markerHeader := MarkerHeader{
		Version: MARKET_VERSION,
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

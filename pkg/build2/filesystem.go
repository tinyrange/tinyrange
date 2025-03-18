package build2

import (
	"fmt"
	"io"
	"regexp"

	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/hash"
	"github.com/tinyrange/tinyrange/pkg/log"
)

const (
	definitionFileName = "definition.json"
	receiptFileName    = "receipt.json"
	outputPrefix       = "output."
)

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

type filesystemBuildDirectory struct {
	dir filesystem.MutableDirectory
}

// ReadDefinition implements BuildCacheDirectory.
func (f *filesystemBuildDirectory) ReadDefinition() ([]byte, error) {
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

// OpenOutputFile implements BuildCacheDirectory.
func (f *filesystemBuildDirectory) GetOutputFile(name string) (filesystem.File, error) {
	file, err := f.dir.GetChild(outputPrefix + name)
	if err != nil {
		return nil, err
	}

	return file.File, nil
}

// ReadReceipt implements BuildCacheDirectory.
func (f *filesystemBuildDirectory) ReadReceipt() ([]byte, error) {
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
	_ common.BuildCacheDirectory = &filesystemBuildDirectory{}
)

type filesystemBuildCache struct {
	dir filesystem.MutableDirectory
}

// GetHostFilename implements BuildCacheFilesystem.
func (f *filesystemBuildCache) GetHostFilename() (string, error) {
	return filesystem.GetHostFilename(f.dir)
}

// CreateBuildDirectory implements BuildCacheFilesystem.
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

	return &filesystemBuildDirectory{dir: buildDir}, nil
}

// GetBuildDirectory implements BuildCacheFilesystem.
func (f *filesystemBuildCache) GetBuildDirectory(hash hash.Hash) (common.BuildCacheDirectory, error) {
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

	return &filesystemBuildDirectory{dir: buildDir}, nil
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
	_ common.BuildCacheFilesystem = &filesystemBuildCache{}
)

func NewFilesystemBuildCache(dir filesystem.MutableDirectory) common.BuildCacheFilesystem {
	return &filesystemBuildCache{dir: dir}
}

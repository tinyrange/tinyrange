package builder

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"time"

	"github.com/tinyrange/tinyrange/pkg/archive2"
	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/filesystem/cvmfs"
	"github.com/tinyrange/tinyrange/pkg/hash"
	"go.starlark.net/starlark"
)

type fetchCvmfsDefinition struct {
	params FetchCVMFSParameters
}

// implements common.BuildDefinition.
func (def *fetchCvmfsDefinition) Params() hash.SerializableValue { return def.params }
func (def *fetchCvmfsDefinition) SerializableType() string       { return "FetchCvmfsDefinition" }
func (def *fetchCvmfsDefinition) Create(params hash.SerializableValue) hash.Definition {
	return &fetchCvmfsDefinition{params: params.(FetchCVMFSParameters)}
}

// Build implements common.StarBuildDefinition.
func (def *fetchCvmfsDefinition) Build(ctx common.BuildContext) error {
	repo := cvmfs.NewRepository(ctx, def.params.Mirror, def.params.Repo)

	index, err := ctx.CreateFile("index")
	if err != nil {
		return err
	}
	defer index.Close()

	contents, err := ctx.CreateFile("contents")
	if err != nil {
		return err
	}
	defer contents.Close()

	ark, err := archive2.NewArchiveWriter(index, contents)
	if err != nil {
		return err
	}

	files, err := repo.GetAllFilesWithPrefix(def.params.Path)
	if err != nil {
		return err
	}

	for _, file := range files {
		kind := file.Kind()

		var fac archive2.EntryFactory
		var reader io.Reader

		switch kind {
		case filesystem.TypeDirectory:
			fac = *fac.Kind(archive2.EntryKindDirectory).
				Name(file.FullPath).
				Size(0).
				Mode(fs.FileMode(file.Mode)).
				Owner(int(file.Uid), int(file.Gid)).
				ModTime(time.Unix(file.Mtime, 0))
		case filesystem.TypeSymlink:
			fac = *fac.Kind(archive2.EntryKindSymlink).
				Name(file.FullPath).
				Size(0).
				Mode(fs.FileMode(file.Mode)).
				Owner(int(file.Uid), int(file.Gid)).
				ModTime(time.Unix(file.Mtime, 0)).
				Linkname(file.Symlink)
		case filesystem.TypeRegular:
			var metadata archive2.CVMFSArchiveMetadata

			urls, err := ctx.Database().UrlsFor(def.params.Mirror)
			if err != nil {
				return fmt.Errorf("failed to get urls for mirror: %w", err)
			}

			metadata.Kind = archive2.CVMFS_ARCHIVE_METADATA_KIND
			metadata.Mirrors = urls
			metadata.Repo = def.params.Repo

			if file.IsChunked() {
				for _, chunk := range file.Chunks {
					metadata.Chunks = append(metadata.Chunks, archive2.CVMFSArchiveChunk{
						Hash:   hex.EncodeToString(chunk.Hash),
						Offset: chunk.Offset,
						Size:   chunk.Size,
					})
				}
			} else {
				metadata.Chunks = append(metadata.Chunks, archive2.CVMFSArchiveChunk{
					Hash:   hex.EncodeToString(file.Hash),
					Offset: 0,
					Size:   file.Size,
				})
			}

			metadataMarshaled, err := json.Marshal(metadata)
			if err != nil {
				return fmt.Errorf("failed to marshal metadata: %w", err)
			}

			fac = *fac.Kind(archive2.EntryKindExtended).
				Name(file.FullPath).
				Size(int64(len(metadataMarshaled))).
				Mode(fs.FileMode(file.Mode)).
				Owner(int(file.Uid), int(file.Gid)).
				ModTime(time.Unix(file.Mtime, 0))

			reader = bytes.NewReader(metadataMarshaled)
		default:
			return fmt.Errorf("unknown kind: %s", kind)
		}

		if err := ark.WriteEntry(&fac, reader); err != nil {
			return fmt.Errorf("failed to write entry: %w", err)
		}
	}

	return nil
}

// Dependencies implements common.StarBuildDefinition.
func (def *fetchCvmfsDefinition) Dependencies() ([]common.BuildDefinition, error) {
	return []common.BuildDefinition{}, nil
}

// NeedsBuild implements common.StarBuildDefinition.
func (def *fetchCvmfsDefinition) NeedsBuild(ctx common.BuildContext) (bool, error) {
	return false, nil
}

// ToStarlark implements common.StarBuildDefinition.
func (def *fetchCvmfsDefinition) ToStarlark(artifact common.BuildArtifact) (starlark.Value, error) {
	return starlark.None, fmt.Errorf("ToStarlark on FetchCvmfsDefinition is not implemented")
}

// implements starlark.Value.
func (def *fetchCvmfsDefinition) String() string {
	return fmt.Sprintf("FetchCvmfsDefinition_%s_%s_%s", def.params.Mirror, def.params.Repo, def.params.Path)
}
func (*fetchCvmfsDefinition) Type() string { return "FetchCvmfsDefinition" }
func (*fetchCvmfsDefinition) Hash() (uint32, error) {
	return 0, fmt.Errorf("FetchCvmfsDefinition is not hashable")
}
func (*fetchCvmfsDefinition) Truth() starlark.Bool { return starlark.True }
func (*fetchCvmfsDefinition) Freeze()              {}

func newFetchCvmfsDefinition(mirror, repo, path string) common.StarBuildDefinition {
	return &fetchCvmfsDefinition{
		params: FetchCVMFSParameters{
			Mirror: mirror,
			Repo:   repo,
			Path:   path,
		},
	}
}

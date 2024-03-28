package db

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/filesystem"
	"go.starlark.net/starlark"
)

type GitCommit struct {
	commit *object.Commit
}

// Get implements starlark.Mapping.
func (g *GitCommit) Get(k starlark.Value) (v starlark.Value, found bool, err error) {
	name, _ := starlark.AsString(k)

	f, err := g.commit.File(name)
	if err == object.ErrFileNotFound {
		return starlark.None, false, nil
	} else if err != nil {
		return starlark.None, false, err
	}

	r, err := f.Reader()
	if err != nil {
		return starlark.None, false, err
	}

	return &StarFile{f: r}, true, nil
}

func (*GitCommit) String() string { return "GitCommit" }
func (*GitCommit) Type() string   { return "GitCommit" }
func (*GitCommit) Hash() (uint32, error) {
	return 0, fmt.Errorf("GitCommit is not hashable")
}
func (*GitCommit) Truth() starlark.Bool { return starlark.True }
func (*GitCommit) Freeze()              {}

var (
	_ starlark.Value   = &GitCommit{}
	_ starlark.Mapping = &GitCommit{}
)

type GitRepository struct {
	repo *git.Repository
}

// Get implements starlark.Mapping.
func (g *GitRepository) Get(k starlark.Value) (v starlark.Value, found bool, err error) {
	commit, _ := starlark.AsString(k)

	obj, err := g.repo.CommitObject(plumbing.NewHash(commit))
	if err == plumbing.ErrObjectNotFound {
		return starlark.None, false, nil
	} else if err != nil {
		return starlark.None, false, err
	}

	return &GitCommit{commit: obj}, true, nil
}

func (*GitRepository) String() string { return "GitRepository" }
func (*GitRepository) Type() string   { return "GitRepository" }
func (*GitRepository) Hash() (uint32, error) {
	return 0, fmt.Errorf("GitRepository is not hashable")
}
func (*GitRepository) Truth() starlark.Bool { return starlark.True }
func (*GitRepository) Freeze()              {}

var (
	_ starlark.Value   = &GitRepository{}
	_ starlark.Mapping = &GitRepository{}
)

func (db *PackageDatabase) fetchGit(url string) (*GitRepository, error) {
	cachePath, err := db.Eif.GetCachePath(url)
	if err != nil {
		return nil, err
	}

	slog.Info("downloading with git", "url", url)

	repo, err := git.PlainClone(cachePath, true, &git.CloneOptions{
		URL:      url,
		Progress: os.Stdout,
	})
	if err == git.ErrRepositoryAlreadyExists {
		store := osfs.New(cachePath)

		s := filesystem.NewStorage(store, cache.NewObjectLRUDefault())

		repo, err = git.Open(s, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to open: %s", err)
		}

		err = repo.Fetch(&git.FetchOptions{RemoteURL: url, Progress: os.Stdout})
		if err == git.NoErrAlreadyUpToDate {
			// fallthrough
		} else if err != nil {
			return nil, fmt.Errorf("failed to fetch: %s", err)
		}
	} else if err != nil {
		return nil, fmt.Errorf("failed to clone: %s", err)
	}

	return &GitRepository{repo: repo}, nil
}

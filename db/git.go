package db

import (
	"fmt"
	"log/slog"
	"os"
	"path"
	"time"

	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/filesystem"
	"go.starlark.net/starlark"
)

type gitTreeIterator struct {
	tree  *object.Tree
	name  string
	ents  []object.TreeEntry
	index int
}

// Done implements starlark.Iterator.
func (g *gitTreeIterator) Done() {
	g.index = len(g.ents)
}

// Next implements starlark.Iterator.
func (g *gitTreeIterator) Next(p *starlark.Value) bool {
	if g.index == len(g.ents) {
		return false
	}

	node := g.ents[g.index]

	if node.Mode.IsFile() {
		// assume a file.
		file, err := g.tree.File(node.Name)
		if err != nil {
			*p = starlark.None
			return false
		}

		r, err := file.Reader()
		if err != nil {
			*p = starlark.None
			return false
		}

		*p = &StarFile{f: r, name: path.Join(g.name, node.Name)}
	} else {
		// assume a directory.
		tree, err := g.tree.Tree(node.Name)
		if err != nil {
			*p = starlark.None
			return false
		}

		*p = &GitTree{tree: tree, name: path.Join(g.name, node.Name)}
	}

	g.index += 1
	return g.index != len(g.ents)
}

var (
	_ starlark.Iterator = &gitTreeIterator{}
)

type GitTree struct {
	tree *object.Tree
	name string
}

// Attr implements starlark.HasAttrs.
func (g *GitTree) Attr(name string) (starlark.Value, error) {
	if name == "name" {
		return starlark.String(g.name), nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (g *GitTree) AttrNames() []string {
	return []string{"name"}
}

// Iterate implements starlark.Iterable.
func (g *GitTree) Iterate() starlark.Iterator {
	return &gitTreeIterator{tree: g.tree, name: g.name, ents: g.tree.Entries}
}

// Get implements starlark.Mapping.
func (g *GitTree) Get(k starlark.Value) (v starlark.Value, found bool, err error) {
	name, _ := starlark.AsString(k)

	f, err := g.tree.File(name)
	if err == object.ErrFileNotFound {
		child, err := g.tree.Tree(name)
		if err == object.ErrDirectoryNotFound {
			return starlark.None, false, nil
		} else if err != nil {
			return starlark.None, false, err
		}

		return &GitTree{tree: child, name: path.Join(g.name, name)}, true, nil
	} else if err != nil {
		return starlark.None, false, err
	}

	r, err := f.Reader()
	if err != nil {
		return starlark.None, false, err
	}

	return &StarFile{f: r, name: path.Join(g.name, name)}, true, nil
}

func (t *GitTree) String() string { return fmt.Sprintf("GitTree{%s}", t.name) }
func (*GitTree) Type() string     { return "GitTree" }
func (*GitTree) Hash() (uint32, error) {
	return 0, fmt.Errorf("GitTree is not hashable")
}
func (*GitTree) Truth() starlark.Bool { return starlark.True }
func (*GitTree) Freeze()              {}

var (
	_ starlark.Value    = &GitTree{}
	_ starlark.HasAttrs = &GitTree{}
	_ starlark.Mapping  = &GitTree{}
	_ starlark.Iterable = &GitTree{}
)

type GitRepository struct {
	repo *git.Repository
}

// Attr implements starlark.HasAttrs.
func (g *GitRepository) Attr(name string) (starlark.Value, error) {
	if name == "tag" {
		return starlark.NewBuiltin("GitRepository.tag", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				tag string
			)

			if err := starlark.UnpackArgs("GitRepository.tag", args, kwargs,
				"tag", &tag,
			); err != nil {
				return starlark.None, err
			}

			obj, err := g.repo.Tag(tag)
			if err != nil {
				return starlark.None, err
			}

			tagObj, err := g.repo.TagObject(obj.Hash())
			if err != nil {
				return starlark.None, err
			}

			commit, err := tagObj.Commit()
			if err != nil {
				return starlark.None, err
			}

			tree, err := commit.Tree()
			if err != nil {
				return starlark.None, err
			}

			return &GitTree{tree: tree}, nil
		}), nil
	} else if name == "branch" {
		return starlark.NewBuiltin("GitRepository.branch", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				branch string
			)

			if err := starlark.UnpackArgs("GitRepository.branch", args, kwargs,
				"branch", &branch,
			); err != nil {
				return starlark.None, err
			}

			obj, err := g.repo.Branch(branch)
			if err != nil {
				return starlark.None, err
			}

			ref, err := g.repo.Reference(obj.Merge, true)
			if err != nil {
				return starlark.None, err
			}

			commit, err := g.repo.CommitObject(ref.Hash())
			if err != nil {
				return starlark.None, err
			}

			tree, err := commit.Tree()
			if err != nil {
				return starlark.None, err
			}

			return &GitTree{tree: tree}, nil
		}), nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (g *GitRepository) AttrNames() []string {
	return []string{"tag", "branch"}
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

	tree, err := obj.Tree()
	if err != nil {
		return starlark.None, false, err
	}

	return &GitTree{tree: tree}, true, nil
}

func (*GitRepository) String() string { return "GitRepository" }
func (*GitRepository) Type() string   { return "GitRepository" }
func (*GitRepository) Hash() (uint32, error) {
	return 0, fmt.Errorf("GitRepository is not hashable")
}
func (*GitRepository) Truth() starlark.Bool { return starlark.True }
func (*GitRepository) Freeze()              {}

var (
	_ starlark.Value    = &GitRepository{}
	_ starlark.Mapping  = &GitRepository{}
	_ starlark.HasAttrs = &GitRepository{}
)

func (db *PackageDatabase) fetchGit(url string) (*GitRepository, error) {
	cachePath, err := db.Eif.GetCachePath(url)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(cachePath)
	if err == nil {
		modTime := info.ModTime()

		if time.Since(modTime) < 8*time.Hour {
			store := osfs.New(cachePath)

			s := filesystem.NewStorage(store, cache.NewObjectLRUDefault())

			repo, err := git.Open(s, nil)
			if err != nil {
				return nil, fmt.Errorf("failed to open: %s", err)
			}

			return &GitRepository{repo: repo}, nil
		}
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

	if err := os.Chtimes(cachePath, time.Now(), time.Now()); err != nil {
		return nil, err
	}

	return &GitRepository{repo: repo}, nil
}

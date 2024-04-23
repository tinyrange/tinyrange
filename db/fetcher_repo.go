package db

import (
	"bytes"
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/schollz/progressbar/v3"
	"github.com/tinyrange/pkg2/core"
	"go.etcd.io/bbolt"
	"go.starlark.net/starlark"
)

func getSha256(val []byte) string {
	sum := sha256.Sum256(val)
	return hex.EncodeToString(sum[:])
}

type RepositoryFetcherStatus int

func (status RepositoryFetcherStatus) String() string {
	switch status {
	case RepositoryFetcherStatusNotLoaded:
		return "Not Loaded"
	case RepositoryFetcherStatusLoading:
		return "Loading"
	case RepositoryFetcherStatusLoaded:
		return "Loaded"
	default:
		return "<unknown>"
	}
}

const (
	RepositoryFetcherStatusNotLoaded RepositoryFetcherStatus = iota
	RepositoryFetcherStatusLoading
	RepositoryFetcherStatusLoaded
	RepositoryFetcherStatusError
)

type logMessage struct {
	Time    time.Time
	Message string
}

func (m logMessage) String() string {
	return fmt.Sprintf("%s %s", m.Time.Format(time.DateTime), m.Message)
}

type RepositoryFetcher struct {
	db                    *PackageDatabase
	Packages              []*Package
	addPackageMutex       sync.Mutex
	Distributions         map[string]bool
	Architectures         map[string]bool
	Distro                string
	Func                  *starlark.Function
	Args                  starlark.Tuple
	Status                RepositoryFetcherStatus
	updateMutex           sync.Mutex
	LastUpdateTime        time.Duration
	LastUpdated           time.Time
	Messages              []logMessage
	Counter               core.Counter
	validArchitectures    map[CPUArchitecture]bool
	enforceSemverVersions bool
}

// Count implements core.Logger.
func (r *RepositoryFetcher) Count(message string) {
	r.Counter.Add(message)
}

// Log implements Logger.
func (r *RepositoryFetcher) Log(message string) {
	r.Messages = append(r.Messages, logMessage{
		Time:    time.Now(),
		Message: message,
	})
}

func (r *RepositoryFetcher) Matches(query PackageName) bool {
	if query.Distribution != "" && r.Distributions != nil {
		_, ok := r.Distributions[query.Distribution]

		return ok
	}

	if query.Architecture != "" && r.Architectures != nil {
		_, ok := r.Architectures[query.Architecture]

		return ok
	}

	return true
}

func (r *RepositoryFetcher) Key() string {
	var tokens []string

	tokens = append(tokens, r.Func.Name())

	for _, arg := range r.Args {
		str, ok := starlark.AsString(arg)
		if !ok {
			str = arg.String()
		}

		tokens = append(tokens, str)
	}

	return getSha256([]byte(strings.Join(tokens, "_")))
}

func (r *RepositoryFetcher) validateName(name PackageName) (PackageName, error) {
	var err error

	if r.validArchitectures == nil {
		r.validArchitectures = map[CPUArchitecture]bool{
			ArchInvalid: true,

			ArchAArch64: true,
			ArchArmHF:   true,
			ArchArmV7:   true,

			ArchMips:        true,
			ArchMipsN32:     true,
			ArchMipsN32EL:   true,
			ArchMipsN32R6:   true,
			ArchMipsN32R6EL: true,
			ArchMipsR6:      true,
			ArchMipsR6EL:    true,
			ArchMipsEL:      true,
			ArchMips64:      true,
			ArchMips64EL:    true,
			ArchMips64R6:    true,
			ArchMips64R6EL:  true,

			ArchPowerPC: true,
			ArchPPC64LE: true,
			ArchPPC64EL: true,

			ArchRiscV64: true,

			ArchS390X: true,

			ArchI386:   true,
			ArchI586:   true,
			ArchI686:   true,
			ArchX32:    true,
			ArchX86_64: true,

			ArchAny:    true,
			ArchSource: true,
		}
	}

	if _, ok := r.validArchitectures[CPUArchitecture(name.Architecture)]; !ok {
		return PackageName{}, fmt.Errorf("invalid architecture: %s", name.Architecture)
	}

	if r.enforceSemverVersions && name.Version != "" {
		oldName := name.Version
		name.Version, err = semverCanonical(name.Version)
		if err != nil {
			r.Counter.Add(fmt.Sprintf("invalid semver: %s", err))
			name.Version = oldName
		}
	}

	return name, nil
}

func (r *RepositoryFetcher) addPackage(name PackageName) (starlark.Value, error) {
	var err error

	r.addPackageMutex.Lock()
	defer r.addPackageMutex.Unlock()

	name, err = r.validateName(name)
	if err != nil {
		return starlark.None, err
	}

	pkg := NewPackage()
	pkg.Name = name
	r.Packages = append(r.Packages, pkg)
	return pkg, nil
}

// Attr implements starlark.HasAttrs.
func (r *RepositoryFetcher) Attr(name string) (starlark.Value, error) {
	if name == "add_package" {
		return starlark.NewBuiltin("Repo.add_package", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				name PackageName
			)

			if err := starlark.UnpackArgs("Repo.add_package", args, kwargs,
				"name", &name,
			); err != nil {
				return starlark.None, err
			}

			return r.addPackage(name)
		}), nil
	} else if name == "name" {
		return starlark.NewBuiltin("Repo.name", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				distribution string
				name         string
				version      string
				architecture string
			)

			if err := starlark.UnpackArgs("Repo.name", args, kwargs,
				"name", &name,
				"version?", &version,
				"distribution?", &distribution,
				"architecture?", &architecture,
			); err != nil {
				return starlark.None, err
			}

			if distribution == "" {
				distribution = r.Distro
			}

			pkgName, err := NewPackageName(distribution, name, version, architecture)
			if err != nil {
				return starlark.None, err
			}

			pkgName, err = r.validateName(pkgName)
			if err != nil {
				return starlark.None, err
			}

			return pkgName, nil
		}), nil
	} else if name == "parallel_for" {
		return starlark.NewBuiltin("Repo.parallel_for", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				target *starlark.List
				f      *starlark.Function
				fArgs  starlark.Tuple
				jobs   int
			)

			if err := starlark.UnpackArgs("Repo.parallel_for", args, kwargs,
				"target", &target,
				"f", &f,
				"fArgs", &fArgs,
				"jobs", &jobs,
			); err != nil {
				return starlark.None, err
			}

			var elements []starlark.Value

			target.Elements(func(v starlark.Value) bool {
				elements = append(elements, v)
				return true
			})

			reqs := make(chan starlark.Value)

			if jobs == 0 {
				jobs = 1
			}

			pb := progressbar.Default(int64(len(elements)))
			defer pb.Close()

			if jobs == 1 {
				for _, element := range elements {
					_, err := starlark.Call(thread, f, append(append(starlark.Tuple{r}, fArgs...), element), []starlark.Tuple{})
					if err != nil {
						if sErr, ok := err.(*starlark.EvalError); ok {
							slog.Error("parallel_for thread got error", "error", sErr, "backtrace", sErr.Backtrace())
						} else {
							slog.Warn("parallel_for thread got error", "error", err)
						}
						return starlark.None, err
					}

					pb.Add(1)
				}
			} else {
				for i := 0; i < jobs; i++ {
					go func() {
						thread := &starlark.Thread{}

						for req := range reqs {
							_, err := starlark.Call(thread, f, append(append(starlark.Tuple{r}, fArgs...), req), []starlark.Tuple{})
							if err != nil {
								if sErr, ok := err.(*starlark.EvalError); ok {
									slog.Error("parallel_for thread got error", "error", sErr, "backtrace", sErr.Backtrace())
								} else {
									slog.Warn("parallel_for thread got error", "error", err)
								}
								return
							}

							pb.Add(1)
						}
					}()
				}

				for _, element := range elements {
					reqs <- element
				}

				close(reqs)
			}

			return starlark.None, nil
		}), nil
	} else if name == "log" {
		return starlark.NewBuiltin("Repo.log", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var message string

			if err := starlark.UnpackArgs("Repo.log", args, kwargs,
				"message", &message,
			); err != nil {
				return starlark.None, err
			}

			r.Log(message)

			return starlark.None, nil
		}), nil
	} else if name == "pledge" {
		return starlark.NewBuiltin("Repo.pledge", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				semverVersion bool
			)

			if err := starlark.UnpackArgs("Repo.pledge", args, kwargs,
				"semver", &semverVersion,
			); err != nil {
				return starlark.None, err
			}

			if semverVersion {
				r.enforceSemverVersions = true
			}

			return starlark.None, nil
		}), nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (*RepositoryFetcher) AttrNames() []string {
	return []string{"add_package", "name", "parallel_for", "log", "pledge"}
}

func (fetcher *RepositoryFetcher) String() string {
	name := fetcher.Func.Name()

	var args []string
	for _, arg := range fetcher.Args {
		args = append(args, arg.String())
	}

	return fmt.Sprintf("%s(%s)", name, strings.Join(args, ", "))
}
func (*RepositoryFetcher) Type() string { return "RepositoryFetcher" }
func (*RepositoryFetcher) Hash() (uint32, error) {
	return 0, fmt.Errorf("RepositoryFetcher is not hashable")
}
func (*RepositoryFetcher) Truth() starlark.Bool { return starlark.True }
func (*RepositoryFetcher) Freeze()              {}

func (fetcher *RepositoryFetcher) fetchWithKey(eif *core.EnvironmentInterface, key string, forceRefresh bool) error {
	// Only allow a single thread to update a fetcher at the same time.
	fetcher.updateMutex.Lock()
	defer fetcher.updateMutex.Unlock()

	// Reset package list.
	fetcher.Packages = []*Package{}
	fetcher.Architectures = nil
	fetcher.Distributions = nil
	fetcher.Messages = []logMessage{}

	// Update Status
	fetcher.LastUpdated = time.Now()
	fetcher.Status = RepositoryFetcherStatusLoading

	expireTime := 2 * time.Hour
	if forceRefresh {
		expireTime = 0
	}

	distributionIndex := map[string]bool{}
	architectureIndex := map[string]bool{}

	err := eif.CacheObjects(
		key, int(PackageMetadataVersionCurrent), expireTime,
		func(write func(obj any) error) error {
			slog.Info("fetching", "fetcher", fetcher.String())

			thread := &starlark.Thread{}

			core.SetLogger(thread, fetcher)

			_, err := starlark.Call(thread, fetcher.Func,
				append(starlark.Tuple{fetcher}, fetcher.Args...),
				[]starlark.Tuple{},
			)
			if err != nil {
				if sErr, ok := err.(*starlark.EvalError); ok {
					slog.Error("got starlark error", "error", sErr, "backtrace", sErr.Backtrace())
				}
				return fmt.Errorf("error calling user callback: %s", err)
			}

			for _, pkg := range fetcher.Packages {
				if err := write(pkg); err != nil {
					return fmt.Errorf("failed to write package: %s", err)
				}
			}

			return nil
		},
		func(read func(obj any) error) error {
			fetcher.Packages = []*Package{}

			for {
				pkg := NewPackage()

				err := read(pkg)
				if err == io.EOF {
					return nil
				} else if err != nil {
					return err
				} else {
					fetcher.Packages = append(fetcher.Packages, pkg)

					// Add to the distribution index.
					distributionIndex[pkg.Name.Distribution] = true

					// Add to the architecture index.
					architectureIndex[pkg.Name.Architecture] = true
				}
			}
		},
	)
	if err != nil {
		fetcher.Status = RepositoryFetcherStatusError

		return err
	}

	if fetcher.db.db != nil {
		fetcher.db.db.Batch(func(tx *bbolt.Tx) error {
			bkt, err := tx.CreateBucketIfNotExists([]byte("PACKAGES"))
			if err != nil {
				return err
			}

			for _, pkg := range fetcher.Packages {
				var buf bytes.Buffer

				enc := gob.NewEncoder(&buf)

				err := enc.Encode(pkg)
				if err != nil {
					return err
				}

				pkgKey := strings.Join(pkg.Name.Path(), "/")

				if err := bkt.Put([]byte(pkgKey), buf.Bytes()); err != nil {
					return err
				}
			}

			return nil
		})
	}

	fetcher.Status = RepositoryFetcherStatusLoaded

	fetcher.Architectures = architectureIndex
	fetcher.Distributions = distributionIndex

	fetcher.LastUpdateTime = time.Since(fetcher.LastUpdated)

	return nil
}

var (
	_ starlark.Value    = &RepositoryFetcher{}
	_ starlark.HasAttrs = &RepositoryFetcher{}
	_ core.Logger       = &RepositoryFetcher{}
)

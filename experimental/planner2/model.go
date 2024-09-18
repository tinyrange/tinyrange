package planner2

import (
	"errors"
)

var (
	ErrAlreadyInstated     = errors.New("already installed")
	ErrIncompatibleVersion = errors.New("incompatible version installed")
	ErrFoundConflict       = errors.New("found conflict")
)

type Directive interface {
	Execute() error
}

type TagList []string

func (t TagList) Matches(other TagList) bool {
	return true
}

type Condition interface {
	Satisfies(name PackageName) (MatchResult, error)
}

type PackageQuery struct {
	Name      string // a empty name matches all.
	Condition Condition
}

func NewPackageQuery(name string) PackageQuery {
	return PackageQuery{Name: name}
}

type PackageOptions []PackageQuery

func NewPackageOptions(opts ...PackageQuery) PackageOptions {
	return opts
}

type PackageName struct {
	Name    string
	Version string
	Tags    TagList
}

func NewName(name string, version string, tags ...string) PackageName {
	return PackageName{Name: name, Version: version, Tags: tags}
}

func (name PackageName) IsZero() bool {
	return name.Name == ""
}

func (name PackageName) Matches(q PackageQuery) (MatchResult, error) {
	if q.Name != "" {
		if q.Name != name.Name {
			return MatchResultNoMatch, nil
		}
	}

	if q.Condition != nil {
		return q.Condition.Satisfies(name)
	}

	return MatchResultMatched, nil
}

type Package interface {
	Name() PackageName
	Aliases() []PackageName
	Installers() ([]Installer, error)
}

type Installer interface {
	Package
	Tags() TagList
	Dependencies() ([]PackageOptions, error)
	Conflicts() ([]PackageQuery, error)
	Directives() ([]Directive, error)
}

type PackageSource interface {
	Find(q PackageQuery) ([]Package, error)
}

type MatchResult string

const (
	MatchResultMatched                MatchResult = "Match"
	MatchResultIncompatibleConditions MatchResult = "IncompatibleConditions"
	MatchResultNoMatch                MatchResult = "NoMatch"
)

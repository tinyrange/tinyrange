package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"runtime/pprof"
	"slices"
	"strings"

	"github.com/tinyrange/tinyrange/experimental/pubgrub"
	"github.com/tinyrange/tinyrange/pkg/builder"
	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/database"
)

type condaRequirement string

// Key implements planner2.Condition.
func (req condaRequirement) Key() string {
	return string(req)
}

func (req condaRequirement) Matches(ver string) bool {
	if req == "" {
		return true
	}

	if strings.HasPrefix(string(req), ">=") {
		reqString := strings.TrimPrefix(string(req), ">=")
		return strings.Compare(ver, reqString) >= 0
	} else if strings.HasPrefix(string(req), "<") {
		reqString := strings.TrimPrefix(string(req), "<")
		return strings.Compare(ver, reqString) < 0
	} else if strings.HasSuffix(string(req), "*") {
		reqString := strings.TrimSuffix(string(req), "*")
		reqString = strings.TrimSuffix(reqString, ".")

		return strings.HasPrefix(ver, reqString)
	} else {
		return ver == string(req)
	}
}

type condaDepend string

func (dep condaDepend) requirements() string {
	_, requirements, ok := strings.Cut(string(dep), " ")
	if !ok {
		return ""
	}

	// Discard build.
	requirements, _, _ = strings.Cut(requirements, " ")

	return requirements
}

// Satisfies implements pubgrub.Condition.
func (dep condaDepend) Satisfies(ver pubgrub.Version) bool {
	requirements := dep.requirements()

	if requirements == "*" {
		return true
	}

	if strings.Contains(requirements, ",") {
		for _, requirement := range strings.Split(requirements, ",") {
			if !condaRequirement(requirement).Matches(ver.String()) {
				return false
			}
		}

		return true
	}

	if strings.Contains(requirements, "|") {
		for _, requirement := range strings.Split(requirements, "|") {
			if condaRequirement(requirement).Matches(ver.String()) {
				return true
			}
		}

		return false
	}

	return condaRequirement(dep.requirements()).Matches(ver.String())
}

// String implements pubgrub.Condition.
func (dep condaDepend) String() string {
	return dep.requirements()
}

func (dep condaDepend) Name() string {
	name, _, _ := strings.Cut(string(dep), " ")

	return name
}

// func (dep condaDepend) Requirements() planner2.Condition {
// 	_, requirements, ok := strings.Cut(string(dep), " ")
// 	if !ok {
// 		return nil
// 	}

// 	// Remove build.
// 	requirements, _, _ = strings.Cut(requirements, " ")

// 	if requirements == "*" {
// 		return planner2.IdentityCondition{}
// 	} else if strings.Contains(requirements, ",") {
// 		var ret planner2.AndCondition

// 		for _, requirement := range strings.Split(requirements, ",") {
// 			ret = append(ret, condaRequirement(requirement))
// 		}

// 		return ret
// 	} else if strings.Contains(requirements, "|") {
// 		var ret planner2.OrCondition

// 		for _, requirement := range strings.Split(requirements, "|") {
// 			ret = append(ret, condaRequirement(requirement))
// 		}

// 		return ret
// 	} else {
// 		return planner2.AndCondition{condaRequirement(requirements)}
// 	}
// }

func (dep condaDepend) Build() string {
	_, requirements, ok := strings.Cut(string(dep), " ")
	if !ok {
		return ""
	}

	_, build, _ := strings.Cut(requirements, " ")

	return build
}

var (
	_ pubgrub.Condition = condaDepend("")
)

type condaPackage struct {
	Build       string        `json:"build"`
	PropName    string        `json:"name"`
	PropVersion string        `json:"version"`
	License     string        `json:"license"`
	Depends     []condaDepend `json:"depends"`

	filename string
}

type condaRepoData struct {
	Packages map[string]condaPackage `json:"packages"`

	index map[string][]condaPackage
}

// GetDependencies implements pubgrub.Source.
func (repo *condaRepoData) GetDependencies(name pubgrub.Name, version pubgrub.Version) ([]pubgrub.Term, error) {
	pkgs, ok := repo.index[string(name)]
	if !ok {
		return nil, fmt.Errorf("package %s not found", name)
	}

	for _, pkg := range pkgs {
		if pkg.PropVersion == version.String() {
			var terms []pubgrub.Term

			for _, dep := range pkg.Depends {
				terms = append(terms, pubgrub.NewTerm(pubgrub.Name(dep.Name()), dep))
			}

			return terms, nil
		}
	}

	return nil, nil
}

// GetVersions implements pubgrub.Source.
func (repo *condaRepoData) GetVersions(name pubgrub.Name) ([]pubgrub.Version, error) {
	pkgs, ok := repo.index[string(name)]
	if !ok {
		return nil, fmt.Errorf("package %s not found", name)
	}

	var versions []pubgrub.Version
	for _, pkg := range pkgs {
		versions = append(versions, pubgrub.SimpleVersion(pkg.PropVersion))
	}

	return versions, nil
}

func (repo *condaRepoData) createIndex() {
	repo.index = make(map[string][]condaPackage)

	for filename, pkg := range repo.Packages {
		pkg.filename = filename
		repo.index[pkg.PropName] = append(repo.index[pkg.PropName], pkg)
	}

	for name := range repo.index {
		idx := repo.index[name]

		slices.SortFunc(idx, func(a condaPackage, b condaPackage) int {
			return strings.Compare(a.PropVersion, b.PropVersion)
		})

		repo.index[name] = idx
	}
}

var (
	_ pubgrub.Source = &condaRepoData{}
)

var (
	cpuprofile = flag.String("cpuprofile", "", "write cpu profile to file")
)

func appMain() error {
	flag.Parse()

	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			return err
		}
		defer f.Close()

		if err := pprof.StartCPUProfile(f); err != nil {
			return err
		}
		defer pprof.StopCPUProfile()
	}

	db := database.New("build/build")

	var sources []pubgrub.Source

	for _, url := range []string{
		"https://conda.anaconda.org/conda-forge/linux-64/repodata.json",
		"https://conda.anaconda.org/conda-forge/noarch/repodata.json",
	} {
		def := builder.NewFetchHttpBuildDefinition(url, 0, nil)

		f, err := db.Build(db.NewBuildContext(def), def, common.BuildOptions{})
		if err != nil {
			return err
		}

		fh, err := f.Open()
		if err != nil {
			return err
		}
		defer fh.Close()

		var data condaRepoData

		if err := json.NewDecoder(fh).Decode(&data); err != nil {
			return err
		}

		data.createIndex()

		slog.Info("loaded", "pkgs", len(data.Packages))

		sources = append(sources, &data)
	}

	root := pubgrub.NewRootSource()

	root.AddPackage("matplotlib", nil)

	sources = append(sources, root)

	solver := pubgrub.NewSolver(sources...)

	versions, err := solver.Solve(root.Term())
	if err != nil {
		return err
	}

	for _, version := range versions {
		slog.Info("version", "version", version)
	}

	return nil
}

func main() {
	if err := appMain(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

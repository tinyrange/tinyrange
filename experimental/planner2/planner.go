package planner2

import (
	"fmt"
)

type installationTree struct {
	Query     PackageOptions
	Installed PackageName
	Children  []*installationTree
}

type InstallationPlan struct {
	parent            *InstallationPlan
	sources           []PackageSource
	tags              TagList
	installedPackages map[string]PackageName
	installationTree  []*installationTree
	directives        []Directive
}

func (plan *InstallationPlan) childPlan() *InstallationPlan {
	return &InstallationPlan{
		parent:            plan,
		sources:           plan.sources,
		installedPackages: make(map[string]PackageName),
	}
}

func (plan *InstallationPlan) copyFrom(child *InstallationPlan) error {
	for k, v := range child.installedPackages {
		plan.installedPackages[k] = v
	}

	plan.directives = append(plan.directives, child.directives...)

	return nil
}

func (plan *InstallationPlan) addName(name PackageName) error {
	if name.IsZero() {
		return fmt.Errorf("name is zero")
	}

	plan.installedPackages[name.Name] = name

	return nil
}

func (plan *InstallationPlan) GetVersionFromName(s string) (PackageName, error) {
	// Check this plan first.
	name, ok := plan.installedPackages[s]
	if ok {
		return name, nil
	}

	// Check the parents next.
	if plan.parent != nil {
		return plan.parent.GetVersionFromName(s)
	}

	// Otherwise return zero.
	return PackageName{}, nil
}

func (plan *InstallationPlan) HasQuery(q PackageOptions) (PackageName, MatchResult, error) {
	for _, opt := range q {
		ver, err := plan.GetVersionFromName(opt.Name)
		if err != nil {
			return PackageName{}, "", err
		}

		if ver.IsZero() {
			continue
		}

		ret, err := ver.Matches(opt)
		if err != nil {
			return PackageName{}, "", err
		}

		return ver, ret, nil
	}

	return PackageName{}, MatchResultNoMatch, nil
}

// This function is effectively a force installation.
// The only reason it can fail is if a conflict is found.
func (plan *InstallationPlan) install(tree *installationTree, install Installer) (*installationTree, error) {
	// Check to make sure the tag list matches.
	if !plan.tags.Matches(install.Tags()) {
		return tree, ErrFoundConflict
	}

	// Check for conflicts.
	conflicts, err := install.Conflicts()
	if err != nil {
		return nil, err
	}

	// The conflict check is implemented aa a single query.
	_, result, err := plan.HasQuery(conflicts)
	if err != nil {
		return nil, err
	}

	// We only care about already installed. Incompatible version and not installed are both fine.
	if result == MatchResultMatched {
		return tree, ErrFoundConflict
	}

	// Add the primary name.
	if err := plan.addName(install.Name()); err != nil {
		return nil, err
	}

	// Add any aliases.
	aliases := install.Aliases()
	for _, alias := range aliases {
		if err := plan.addName(alias); err != nil {
			return nil, err
		}
	}

	// Add dependencies.
	depends, err := install.Dependencies()
	if err != nil {
		return nil, err
	}

	for _, depend := range depends {
		tree, err := plan.add(depend)
		if err == ErrAlreadyInstated || err == nil {
			tree.Children = append(tree.Children, tree)

			continue
		}

		return nil, err
	}

	// Finally add directives to the list.
	directives, err := install.Directives()
	if err != nil {
		return nil, err
	}

	plan.directives = append(plan.directives, directives...)

	return tree, nil
}

func (plan *InstallationPlan) add(query PackageOptions) (*installationTree, error) {
	tree := &installationTree{Query: query}

	// check to make sure the query is not already installed.
	name, result, err := plan.HasQuery(query)
	if err != nil {
		return nil, err
	}

	if result == MatchResultMatched {
		tree.Installed = name

		return tree, ErrAlreadyInstated
	} else if result == MatchResultIncompatibleConditions {
		tree.Installed = name

		return tree, ErrIncompatibleVersion
	}

	// Find a list of installation candidates.
	var candidates []Package
	for _, src := range plan.sources {
		for _, opt := range query {
			opts, err := src.Find(opt)
			if err != nil {
				return nil, err
			}

			candidates = append(candidates, opts...)
		}
	}

	// Find the first installable candidate.
	for _, candidate := range candidates {
		// Get a list of all installers.
		installers, err := candidate.Installers()
		if err != nil {
			return nil, err
		}

		for _, installer := range installers {
			// Create a copy of the current installation plan.
			child := plan.childPlan()

			// test installation
			tree, err := child.install(tree, installer)
			if err == ErrFoundConflict || err == ErrIncompatibleVersion {
				continue
			} else if err != nil {
				// Other internal errors are always propagated out.
				return nil, err
			}

			// If the installation succeeds then copy the results back to the main plan.
			if err := plan.copyFrom(child); err != nil {
				return nil, err
			}

			return tree, nil
		}
	}

	return nil, fmt.Errorf("no installation candidates for %s", query)
}

func (plan *InstallationPlan) Add(query PackageOptions) error {
	tree, err := plan.add(query)
	if err == ErrAlreadyInstated || err == nil {
		plan.installationTree = append(plan.installationTree, tree)

		return nil
	}

	return err
}

func NewPlan(sources []PackageSource, tags TagList) *InstallationPlan {
	return &InstallationPlan{
		sources:           sources,
		tags:              tags,
		installedPackages: make(map[string]PackageName),
	}
}

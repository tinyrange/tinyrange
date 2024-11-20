package buildinfo

import (
	"fmt"
	"regexp"
	"strconv"
)

type VersionInfo struct {
	Major int
	Minor int
	Patch int
	Build string
}

func (v VersionInfo) String() string {
	if v.Build == "" {
		return fmt.Sprintf("v%d.%d.%d", v.Major, v.Minor, v.Patch)
	} else {
		return fmt.Sprintf("v%d.%d.%d-%s", v.Major, v.Minor, v.Patch, v.Build)
	}
}

func (v VersionInfo) IsDev() bool {
	return v.Build == "dev"
}

func (v VersionInfo) IsZero() bool {
	return v.Major == 0 && v.Minor == 0 && v.Patch == 0 && v.Build == ""
}

func (v VersionInfo) IsStable() bool {
	return v.Build == ""
}

func (v VersionInfo) GreaterThan(other VersionInfo) bool {
	if v.IsDev() {
		return true
	}

	if v.Major > other.Major {
		return true
	} else if v.Major < other.Major {
		return false
	}

	if v.Minor > other.Minor {
		return true
	} else if v.Minor < other.Minor {
		return false
	}

	if v.Patch > other.Patch {
		return true
	} else if v.Patch < other.Patch {
		return false
	}

	if v.Build != "" && other.Build == "" {
		return true
	} else if v.Build == "" && other.Build != "" {
		return false
	}

	return false
}

var versionRegex = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)(?:-([\w]+))?$`)

func ParseVersion(s string) (VersionInfo, error) {
	if s == "dev" {
		return VersionInfo{Build: "dev"}, nil
	}

	matches := versionRegex.FindStringSubmatch(s)
	if matches == nil {
		return VersionInfo{}, fmt.Errorf("invalid version string: %s", s)
	}

	atoi := func(s string) int {
		n, _ := strconv.Atoi(s)
		return n
	}

	return VersionInfo{
		Major: atoi(matches[1]),
		Minor: atoi(matches[2]),
		Patch: atoi(matches[3]),
		Build: matches[4],
	}, nil
}

func Version() VersionInfo {
	ver, _ := ParseVersion(VERSION)
	return ver
}

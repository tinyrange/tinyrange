package db

import (
	"fmt"
	"strings"

	"golang.org/x/mod/semver"
)

func semverCanonical(version string) (string, error) {
	original := version

	if version[0] == '>' {
		version = version[1:]
	} else if version[0] == '<' {
		version = version[1:]
	} else if version[0] == '~' {
		version = version[1:]
	}

	version = strings.TrimPrefix(version, "v")

	// See if the basic changes were sufficient.
	if semver.Canonical("v"+version) != "" {
		return "v" + version, nil
	}

	// Look for a build string.
	build := ""
	if strings.Contains(version, "_") {
		version, build, _ = strings.Cut(version, "_")
	}

	last := version[len(version)-1]
	if last >= 'a' && last <= 'z' {
		version = version[:len(version)-1]
		if build != "" {
			build = string(last) + build
		} else {
			build = string(last)
		}
	}

	// Split tokens and remove any leading zeros.
	tokens := strings.Split(version, ".")
	for i := range tokens {
		tokens[i] = strings.TrimLeft(tokens[i], "0")
		if len(tokens[i]) == 0 {
			tokens[i] = "0"
		}
	}

	// Make sure tokens is at least 3 elements long.
	if len(tokens) == 1 {
		tokens = append(tokens, "0")
	}
	if len(tokens) == 2 {
		tokens = append(tokens, "0")
	}

	// Move any remaining tokens into the build string.
	if len(tokens) > 3 {
		tokens = tokens[:3]
		if build != "" {
			build = build + "." + strings.Join(tokens[3:], ".")
		} else {
			build = strings.Join(tokens[3:], ".")
		}
	}

	// Join the new version.
	version = strings.Join(tokens, ".")

	// Add the build on the end.
	if build != "" {
		version = version + "+" + build
	}

	// Check the new version
	if semver.Canonical("v"+version) != "" {
		return "v" + version, nil
	}

	return "", fmt.Errorf("could not canonicalize: old=%s new=%s", original, version)
}

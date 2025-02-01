package feature

import "log/slog"

// Simple Feature Flag system

type Feature string

const (
	FeatureTranslateShell Feature = "translate_shell"
	FeatureRosetta        Feature = "rosetta"   // (darwin only) Use Rosetta 2 for emulation
	FeatureVz             Feature = "vz"        // (darwin only) Use VZ instead of QEMU
	FeatureBuild1         Feature = "build1"    // Use build1 for building instead of build2
	Feature9P             Feature = "9p"        // Use 9p for file sharing
	FeatureBuildOci       Feature = "build_oci" // Enable the build-oci command
	FeatureSlowBoot       Feature = "slow_boot"
)

var features = make(map[Feature]bool)

func init() {
	features[FeatureTranslateShell] = false
	features[FeatureRosetta] = false
	features[FeatureVz] = false
	features[FeatureBuild1] = false
	features[Feature9P] = true
	features[FeatureSlowBoot] = false
	features[FeatureBuildOci] = false
}

func SetFeaturesFromExperimentalFlags(flags []string) {
	for _, flag := range flags {
		feat := Feature(flag)
		val, ok := features[feat]
		if !ok {
			slog.Info("enabling unknown feature", "feature", feat)
			features[feat] = true
		} else {
			if val {
				slog.Info("disabling feature", "feature", feat)
				features[feat] = false
			} else {
				slog.Info("enabling feature", "feature", feat)
				features[feat] = true
			}
		}
	}
}

func HasFeature(f Feature) bool {
	return features[f]
}

func GetFeatureFlags() []string {
	var flags []string
	for f, v := range features {
		if v {
			flags = append(flags, string(f))
		}
	}
	return flags
}

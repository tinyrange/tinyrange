package feature

import "github.com/tinyrange/tinyrange/pkg/log"

// Simple Feature Flag system

type Feature string

const (
	FeatureRosetta          Feature = "rosetta"            // (darwin only) Use Rosetta 2 for emulation
	FeatureVz               Feature = "vz"                 // (darwin only) Use VZ instead of QEMU
	FeatureBuild1           Feature = "build1"             // Use build1 for building instead of build2
	Feature9P               Feature = "9p"                 // Use 9p for file sharing
	Feature9PVerbose        Feature = "9p_verbose"         // Enable verbose 9p logging of all messages
	FeatureBuildOci         Feature = "build_oci"          // Enable the build-oci command
	FeatureSlowBoot         Feature = "slow_boot"          // Disable boot caching
	FeatureTokenLockerDebug Feature = "token_locker_debug" // Enable debug logging for the token locker
	FeatureFastWritePersist Feature = "fast_write_persist" // Enable fast writes of persistent filesystems.
	FeatureExt4Resize       Feature = "ext4_resize"        // Enable ext4 filesystem resizing
	FeatureNewDiskFormat    Feature = "new_disk_format"    // Enable the new disk format
	FeatureNoAccelerate     Feature = "no_accelerate"      // Disable hardware virtualization acceleration
)

var features = make(map[Feature]bool)

func init() {
	features[FeatureRosetta] = false
	features[FeatureVz] = false
	features[FeatureBuild1] = false
	features[Feature9P] = true
	features[Feature9PVerbose] = false
	features[FeatureSlowBoot] = false
	features[FeatureBuildOci] = false
	features[FeatureTokenLockerDebug] = false
	features[FeatureFastWritePersist] = true
	features[FeatureExt4Resize] = false
	features[FeatureNewDiskFormat] = true
	features[FeatureNoAccelerate] = false
}

func SetFeaturesFromExperimentalFlags(flags []string) {
	for _, flag := range flags {
		feat := Feature(flag)
		val, ok := features[feat]
		if !ok {
			log.Info("enabling unknown feature", "feature", feat)
			features[feat] = true
		} else {
			if val {
				log.Info("disabling feature", "feature", feat)
				features[feat] = false
			} else {
				log.Info("enabling feature", "feature", feat)
				features[feat] = true
			}
		}
	}
}

func HasFeature(f Feature) bool {
	return features[f]
}

func ToggleFeature(f Feature) {
	features[f] = !features[f]
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

//go:build !linux && !darwin && !windows

package vmm

func SupportsAcceleration() bool {
	return false
}

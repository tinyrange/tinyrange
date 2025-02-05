//go:build !linux && !darwin && !windows

package accelerate

func SupportsAcceleration() bool {
	return false
}

//go:build !linux && !freebsd && !openbsd && !dragonfly && !netbsd && !darwin && !windows

package memory

func sysTotalMemory() (uint64, error) {
	return 0, ErrNotSupported
}

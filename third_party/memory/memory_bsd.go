//go:build freebsd || openbsd || dragonfly || netbsd

package memory

func sysTotalMemory() (uint64, error) {
	s, err := sysctlUint64("hw.physmem")
	if err != nil {
		return 0, err
	}
	return s, nil
}

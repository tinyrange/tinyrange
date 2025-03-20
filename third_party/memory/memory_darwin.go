//go:build darwin

package memory

func sysTotalMemory() (uint64, error) {
	s, err := sysctlUint64("hw.memsize")
	if err != nil {
		return 0, err
	}
	return s, nil
}

//go:build linux

package accelerate

import (
	"os"
)

func SupportsAcceleration() bool {
	f, err := os.OpenFile("/dev/kvm", os.O_RDWR, os.ModePerm)
	if err != nil {
		return false
	}
	defer f.Close()

	return true
}

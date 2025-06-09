//go:build linux

package accelerate

import (
	"os"

	"github.com/tinyrange/tinyrange/pkg/log"
)

func SupportsAcceleration(log log.Handler) bool {
	f, err := os.OpenFile("/dev/kvm", os.O_RDWR, os.ModePerm)
	if err != nil {
		return false
	}
	defer f.Close()

	return true
}

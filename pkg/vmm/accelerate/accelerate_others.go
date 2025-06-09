//go:build !linux && !darwin && !windows

package accelerate

import "github.com/tinyrange/tinyrange/pkg/log"

func SupportsAcceleration(log log.Handler) bool {
	return false
}

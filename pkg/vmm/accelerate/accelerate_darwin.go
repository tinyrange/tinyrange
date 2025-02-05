//go:build darwin

package accelerate

import (
	"os/exec"
)

func SupportsAcceleration() bool {
	out, err := exec.Command("sysctl", "kern.hv.supported").Output()
	if err != nil {
		return false
	}

	return string(out) == "kern.hv.supported: 1\n"
}

//go:build darwin

package vm

import (
	"os/exec"
)

func (vm *VirtualMachine) Accelerate() bool {
	if !vm.architecture.IsNative() {
		return false
	}

	out, err := exec.Command("sysctl", "kern.hv.supported").Output()
	if err != nil {
		return false
	}

	return string(out) == "kern.hv.supported: 1"
}

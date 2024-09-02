//go:build linux

package vm

import (
	"os"
)

func (vm *VirtualMachine) Accelerate() bool {
	if !vm.architecture.IsNative() {
		return false
	}

	f, err := os.OpenFile("/dev/kvm", os.O_RDWR, os.ModePerm)
	if err != nil {
		return false
	}
	defer f.Close()

	return true
}

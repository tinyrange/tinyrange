//go:build !linux && !darwin && !windows

package vm

func (vm *VirtualMachine) Accelerate() bool {
	return false
}

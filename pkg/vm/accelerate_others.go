//go:build !linux && !darwin

package vm

func (vm *VirtualMachine) Accelerate() bool {
	return false
}

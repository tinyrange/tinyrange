//go:build linux && arm64

package kvm

import (
	"fmt"
	"unsafe"
)

const (
	RegArm64X0 RegisterId = 0x6030000000100000
	RegArm64SP RegisterId = 0x603000000010003e
	RegArm64PC RegisterId = 0x6030000000100040

	ArmVCPUPowerOff       = 0 // CPU is started in OFF state
	ArmVCPUEl132bit       = 1 // CPU running a 32bit VM
	ArmVCPUPsci02         = 2 // CPU uses PSCI v0.2
	ArmVCPUPmuV3          = 3 // Support guest PMUv3
	ArmVCPUSve            = 4 // enable SVE for this CPU
	ArmVCPUPtrauthAddress = 5 // VCPU uses address authentication
	ArmVCPUPtrauthGeneric = 6 // VCPU uses generic authentication
	ArmVCPUHasEl2         = 7 // Support nested virtualization
	ArmVCPUHasEl2E2h0     = 8 // Limit NV support to E2H RES0
)

type KVMVirtualCPUARMInit struct {
	Target   uint32
	Features [7]uint32
}

func (vcpu *KVMVirtualMachine) ArmGetPreferredTarget() (KVMVirtualCPUARMInit, error) {
	var target KVMVirtualCPUARMInit
	if _, err := ioctl(vcpu.fd, iior(kvmARMPreferredTarget, 4*8), uintptr(unsafe.Pointer(&target))); err != 0 {
		return KVMVirtualCPUARMInit{}, err
	}
	return target, nil
}

func (vcpu *KVMVirtualCPU) ArmInitialize(target KVMVirtualCPUARMInit) error {
	if _, err := ioctl(vcpu.fd, iiow(kvmARMVCPUInit, 4*8), uintptr(unsafe.Pointer(&target))); err != 0 {
		return err
	}
	return nil
}

func (vcpu *KVMVirtualCPU) ArmFinalize(value int) error {
	if _, err := ioctl(vcpu.fd, iiow(kvmARMVCPUFinalize, 4), uintptr(unsafe.Pointer(&value))); err != 0 {
		return fmt.Errorf("kvm_arm_finalize: ioctl failed: %w(%d)", err, err)
	}
	return nil
}

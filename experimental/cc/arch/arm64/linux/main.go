package main

import (
	"fmt"
	"os"
	"runtime"

	"github.com/tinyrange/tinyrange/experimental/cc/linux/kvm"
	"github.com/tinyrange/tinyrange/pkg/log"
)

func appMain() error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	dev, err := kvm.OpenKVMDevice()
	if err != nil {
		return fmt.Errorf("failed to open KVM device: %w", err)
	}
	defer dev.Close()

	vm, err := dev.CreateVM()
	if err != nil {
		return fmt.Errorf("failed to create VM: %w", err)
	}
	defer vm.Close()

	vcpu, err := vm.CreateVCPU()
	if err != nil {
		return fmt.Errorf("failed to create VCPU: %w", err)
	}
	defer vcpu.Close()

	prefered, err := vm.ArmGetPreferredTarget()
	if err != nil {
		return fmt.Errorf("failed to get preferred target: %w", err)
	}

	if err := vcpu.ArmInitialize(prefered); err != nil {
		return fmt.Errorf("failed to initialize VCPU: %w", err)
	}

	pc, err := vcpu.GetRegister(kvm.RegArm64PC)
	if err != nil {
		return fmt.Errorf("failed to get PC register: %w", err)
	}
	log.Info("PC register value", "value", pc)

	if err := vcpu.SetRegister(kvm.RegArm64PC, pc); err != nil {
		return fmt.Errorf("failed to set PC register: %w", err)
	}

	return fmt.Errorf("not implemented")
}

func main() {
	if err := appMain(); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

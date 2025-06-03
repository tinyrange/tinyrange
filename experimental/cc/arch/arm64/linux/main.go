//go:build linux && arm64

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

	mem, err := vm.AllocateMemory(0x40000000, 0x20000, 0)
	if err != nil {
		return fmt.Errorf("failed to allocate memory: %w", err)
	}

	program, err := os.ReadFile("payload.bin")
	if err != nil {
		return fmt.Errorf("failed to read payload file: %w", err)
	}
	copy(mem, program)

	mem[0x10000] = 0x42

	preferred, err := vm.ArmGetPreferredTarget()
	if err != nil {
		return fmt.Errorf("failed to get preferred target: %w", err)
	}

	// check_vm_extension(KVM_CAP_ARM_PSCI_0_2, "KVM_CAP_ARM_PSCI_0_2")
	preferred.Features[0] |= 1 << kvm.ArmVCPUPsci02

	if err := vcpu.ArmInitialize(preferred); err != nil {
		return fmt.Errorf("failed to initialize VCPU: %w", err)
	}

	// Set up essential ARM64 registers
	if err := vcpu.SetRegister(kvm.RegArm64PC, 0x40000000); err != nil {
		return fmt.Errorf("failed to set PC: %w", err)
	}

	fmt.Println("About to call vcpu.Run()...")

	exit, err := vcpu.Run()
	if err != nil {
		return fmt.Errorf("failed to run VCPU: %w", err)
	}

	if exit.Reason() != kvm.ExitSystemEvent {
		return fmt.Errorf("unexpected exit reason: %s", exit.Reason())
	}
	fmt.Println("VCPU exited with reason:", exit.SystemEvent())

	if mem[0x10000] != 0x43 {
		return fmt.Errorf("unexpected memory value at 0x10000: got 0x%x, want 0x43", mem[0x10000])
	}

	return nil
}

func main() {
	if err := appMain(); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

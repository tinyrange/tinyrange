package kvm

import "testing"

func BenchmarkKVMOpen(b *testing.B) {
	for b.Loop() {
		dev, err := OpenKVMDevice("/dev/kvm")
		if err != nil {
			b.Fatal(err)
		}
		dev.Close()
	}
}

func BenchmarkVMCreate(b *testing.B) {
	dev, err := OpenKVMDevice("/dev/kvm")
	if err != nil {
		b.Fatal(err)
	}
	defer dev.Close()

	for b.Loop() {
		vm, err := dev.CreateVirtualMachine()
		if err != nil {
			b.Fatal(err)
		}
		if err := vm.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

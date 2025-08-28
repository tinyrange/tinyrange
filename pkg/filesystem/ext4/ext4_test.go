package ext4

import (
	"testing"

	"github.com/tinyrange/tinyrange/pkg/filesystem/vm"
)

func BenchmarkCreation(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_vm := vm.NewVirtualMemory(8*1024*1024, 4096)

		fs, err := CreateExt4Filesystem(_vm, 0, _vm.Size())
		if err != nil {
			b.Fatal(err)
		}

		_ = fs
	}
}

func BenchmarkNewVirtualMemory(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = vm.NewVirtualMemory(1*1024*1024*1024, 4096)
	}
}

func BenchmarkLargeCreation(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_vm := vm.NewVirtualMemory(1*1024*1024*1024, 4096)

		fs, err := CreateExt4Filesystem(_vm, 0, _vm.Size())
		if err != nil {
			b.Fatal(err)
		}

		_ = fs
	}
}

func TestLargeFile(t *testing.T) {
	_vm := vm.NewVirtualMemory(2*1024*1024*1024, 4096)

	fs, err := CreateExt4Filesystem(_vm, 0, _vm.Size())
	if err != nil {
		t.Fatal(err)
	}

	region := vm.ZeroRegion(1 * 1024 * 1024 * 1024) // 1 GiB

	if err := fs.CreateFile("/largefile", region); err != nil {
		t.Fatal(err)
	}
}

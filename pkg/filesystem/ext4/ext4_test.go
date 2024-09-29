package ext4

import (
	"testing"

	"github.com/tinyrange/vm"
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

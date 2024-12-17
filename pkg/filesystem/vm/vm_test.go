package vm

import "testing"

func BenchmarkNewVM1GB(b *testing.B) {
	for i := 0; i < b.N; i++ {
		vm := NewVirtualMemory(1*1024*1024*1024, 4096)
		_ = vm
	}
}

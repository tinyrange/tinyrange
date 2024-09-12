//go:build windows

package vm

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

func (vm *VirtualMachine) Accelerate() bool {
	lib, err := windows.LoadLibrary("WinHVPlatform.dll")
	if err != nil {
		return false
	}
	defer windows.FreeLibrary(lib)

	addr, err := windows.GetProcAddress(lib, "WHvGetCapability")
	if err != nil {
		return false
	}

	var buf uint64
	var val uint32

	_, _, winErr := syscall.SyscallN(addr, 0x00, uintptr(unsafe.Pointer(&buf)), unsafe.Sizeof(buf), uintptr(unsafe.Pointer(&val)))
	if winErr != windows.ERROR_SUCCESS {
		return false
	}

	return buf != 0
}

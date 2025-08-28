//go:build windows

package accelerate

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/tinyrange/tinyrange/pkg/log"
)

const WHvCapabilityCodeHypervisorPresent = 0x00000000

func SupportsAcceleration(log log.Handler) bool {
	lib, err := windows.LoadLibrary("WinHVPlatform.dll")
	if err != nil {
		log.Warn("Failed to load WinHVPlatform.dll", "err", err)
		return false
	}
	defer windows.FreeLibrary(lib)

	addr, err := windows.GetProcAddress(lib, "WHvGetCapability")
	if err != nil {
		log.Warn("Failed to get WHvGetCapability address", "err", err)
		return false
	}

	var buf uint64
	var val uint32

	_, _, winErr := syscall.SyscallN(
		addr,
		WHvCapabilityCodeHypervisorPresent,
		uintptr(unsafe.Pointer(&buf)),
		unsafe.Sizeof(buf),
		uintptr(unsafe.Pointer(&val)),
	)
	if winErr == windows.ERROR_INVALID_PARAMETER {
		bufSize := make([]byte, val)

		_, _, winErr = syscall.SyscallN(
			addr,
			WHvCapabilityCodeHypervisorPresent,
			uintptr(unsafe.Pointer(&bufSize)),
			uintptr(val),
			uintptr(unsafe.Pointer(&val)),
		)
		if winErr != windows.ERROR_SUCCESS {
			log.Warn(
				"Failed to call WHvGetCapability",
				"err", winErr,
				"errno", int64(syscall.Errno(winErr)),
				"buf", buf,
				"val", val,
			)
			return false
		}
	}

	if winErr != windows.ERROR_SUCCESS {
		log.Warn(
			"Failed to call WHvGetCapability",
			"err", winErr,
			"errno", int64(syscall.Errno(winErr)),
			"buf", buf,
			"val", val,
		)
		return false
	}

	if buf == 0 {
		log.Warn("WHvGetCapability returned 0")
		return false
	}

	return true
}

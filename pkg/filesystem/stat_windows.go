//go:build windows

package filesystem

import (
	"io/fs"
	"syscall"
)

func GetUidAndGidNative(info fs.FileInfo) (int, int, error) {
	return 0, 0, nil
}

func uniqueIdFromFileInfo(info fs.FileInfo) uint64 {
	if stat, ok := info.Sys().(*syscall.Win32FileAttributeData); ok {
		// WARNING: This is not a unique id, but the number of nanoseconds since the file was created.
		return uint64(stat.CreationTime.Nanoseconds())
	}

	return 0
}

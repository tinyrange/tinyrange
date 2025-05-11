//go:build !(linux || darwin || freebsd || openbsd || netbsd || windows)

package filesystem

import "io/fs"

func GetUidAndGidNative(info fs.FileInfo) (int, int, error) {
	return 0, 0, nil
}

func uniqueIdFromFileInfo(info fs.FileInfo) uint64 {
	return 0
}

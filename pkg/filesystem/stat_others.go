//go:build !(linux || darwin || freebsd || openbsd || netbsd)

package filesystem

import "io/fs"

func GetUidAndGidNative(info fs.FileInfo) (int, int, error) {
	return 0, 0, nil
}

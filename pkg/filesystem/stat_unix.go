//go:build linux || darwin || freebsd || openbsd || netbsd

package filesystem

import (
	"io/fs"
	"syscall"
)

func GetUidAndGidNative(info fs.FileInfo) (int, int, error) {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		return int(stat.Uid), int(stat.Gid), nil
	}

	return 0, 0, nil
}

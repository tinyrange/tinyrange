//go:build !linux

package goboot

func MaybeExecInit() bool {
	return false
}

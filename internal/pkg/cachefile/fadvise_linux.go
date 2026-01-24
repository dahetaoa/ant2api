//go:build linux

package cachefile

import "syscall"

func fadviseDontNeed(fd uintptr, offset int64, length int64) error {
	const fadvDontNeed = 4
	_, _, errno := syscall.Syscall6(
		syscall.SYS_FADVISE64,
		fd,
		uintptr(offset),
		uintptr(length),
		fadvDontNeed,
		0,
		0,
	)
	if errno != 0 {
		return errno
	}
	return nil
}

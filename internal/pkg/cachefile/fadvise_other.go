//go:build !linux

package cachefile

func fadviseDontNeed(fd uintptr, offset int64, length int64) error {
	return nil
}

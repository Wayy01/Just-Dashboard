package sysinfo

import "syscall"

// deviceOf identifies the filesystem a path lives on, so a recursive size walk
// can stop at mount boundaries.
func deviceOf(path string) (uint64, error) {
	var st syscall.Stat_t
	if err := syscall.Lstat(path, &st); err != nil {
		return 0, err
	}
	return uint64(st.Dev), nil
}

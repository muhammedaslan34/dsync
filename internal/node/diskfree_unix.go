//go:build !windows

package node

import "golang.org/x/sys/unix"

// freeSpace returns the bytes available to us on the disk holding dir.
func freeSpace(dir string) (int64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return 0, err
	}
	return int64(st.Bavail) * int64(st.Bsize), nil
}

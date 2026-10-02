//go:build !windows

package config

import "os"

func replaceFile(oldPath, newPath string) error { return os.Rename(oldPath, newPath) }

// syncDir makes a rename durable on filesystems that support syncing a
// directory entry.
func syncDir(path string) error {
	d, err := os.Open(path)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

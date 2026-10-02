//go:build windows

package config

import "golang.org/x/sys/windows"

func replaceFile(oldPath, newPath string) error {
	oldPtr, err := windows.UTF16PtrFromString(oldPath)
	if err != nil {
		return err
	}
	newPtr, err := windows.UTF16PtrFromString(newPath)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(oldPtr, newPtr, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}

// Go has no portable way to open and sync a directory on Windows. The temp
// file itself is still synced before the atomic rename.
func syncDir(string) error { return nil }

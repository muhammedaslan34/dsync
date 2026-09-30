package config

import "golang.org/x/sys/windows"

// downloadsDir is the user's real Downloads folder, which OneDrive or the
// user may have moved away from %USERPROFILE%\Downloads.
func downloadsDir() string {
	if d, err := windows.KnownFolderPath(windows.FOLDERID_Downloads, 0); err == nil && d != "" {
		return d
	}
	return defaultDownloads()
}

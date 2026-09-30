package main

import "golang.org/x/sys/windows"

// systemLocale is the Windows display language, such as "fr-FR".
func systemLocale() string {
	langs, err := windows.GetUserPreferredUILanguages(windows.MUI_LANGUAGE_NAME)
	if err != nil || len(langs) == 0 {
		return ""
	}
	return langs[0]
}

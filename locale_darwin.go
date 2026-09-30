package main

import (
	"os/exec"
	"strings"
)

// systemLocale is the macOS language, such as "fr-FR". Apps opened from
// the Finder don't get LANG, so the system setting is asked for.
func systemLocale() string {
	if l := envLocale(); l != "" {
		return l
	}
	out, err := exec.Command("defaults", "read", "-g", "AppleLanguages").Output()
	if err != nil {
		return ""
	}
	// The output is a list: ( "fr-FR", "en-US" ).
	for _, f := range strings.FieldsFunc(string(out), func(r rune) bool { return strings.ContainsRune("()\", \n\t", r) }) {
		return f
	}
	return ""
}

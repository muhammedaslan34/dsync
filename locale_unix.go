//go:build !windows

package main

import (
	"os"
	"strings"
)

// envLocale is the locale from the environment, such as "fr_FR.UTF-8",
// looked up the way gettext does; "C" and "POSIX" count as unset.
func envLocale() string {
	var l string
	for _, v := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if l = os.Getenv(v); l != "" {
			break
		}
	}
	if l == "" || l == "C" || l == "POSIX" || strings.HasPrefix(l, "C.") {
		return ""
	}
	// LANGUAGE, a preference list such as "fr:en", wins over the locale.
	if list := os.Getenv("LANGUAGE"); list != "" {
		return strings.Split(list, ":")[0]
	}
	return l
}

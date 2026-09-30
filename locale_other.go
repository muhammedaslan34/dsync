//go:build !windows && !darwin

package main

// systemLocale is the locale from the environment, such as "fr_FR.UTF-8".
func systemLocale() string { return envLocale() }

//go:build !linux && !windows

package config

func downloadsDir() string { return defaultDownloads() }

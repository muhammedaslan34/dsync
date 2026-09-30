//go:build !linux && !windows

package main

import "errors"

func autostartSupported() bool { return false }
func appMenuSupported() bool   { return false }
func autostartEnabled() bool   { return false }
func appMenuEnabled() bool     { return false }
func setAutostart(bool) error  { return errors.ErrUnsupported }
func setAppMenu(bool) error    { return errors.ErrUnsupported }

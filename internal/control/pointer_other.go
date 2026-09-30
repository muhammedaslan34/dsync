//go:build !linux && !windows

package control

import "errors"

// macOS only applies pointer speed changes after signing in again, so
// dsync leaves it alone there.
func PointerAdjustable() bool                { return false }
func AdjustPointer(step int) (string, error) { return "", errors.ErrUnsupported }
func RestorePointer(string) error            { return nil }

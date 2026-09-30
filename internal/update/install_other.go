//go:build !linux && !windows

package update

import (
	"context"
	"errors"
)

func Kind() string { return KindManual }

func Install(context.Context, string, string) error {
	return errors.New("download the new version from the release page")
}

func Restart() error { return nil }

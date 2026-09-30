package control

import (
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// GNOME keeps pointer speed in -1..1 (0 is the default), separately for
// mice and touchpads.
var gnomePointerKeys = []string{"org.gnome.desktop.peripherals.mouse", "org.gnome.desktop.peripherals.touchpad"}

// PointerAdjustable reports whether dsync can change the pointer speed here.
func PointerAdjustable() bool {
	if _, err := lookPath("gsettings"); err != nil {
		return false
	}
	_, err := exec.Command("gsettings", "get", gnomePointerKeys[0], "speed").Output()
	return err == nil
}

// AdjustPointer makes the pointer faster (step > 0) or slower and returns
// what RestorePointer needs to put it back.
func AdjustPointer(step int) (string, error) {
	step = clampStep(step)
	if step == 0 {
		return "", nil
	}
	var saved []string
	for _, schema := range gnomePointerKeys {
		out, err := exec.Command("gsettings", "get", schema, "speed").Output()
		if err != nil {
			continue
		}
		cur, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
		if err != nil {
			continue
		}
		next := max(-1, min(1, cur+float64(step)*0.3))
		if err := exec.Command("gsettings", "set", schema, "speed", fmt.Sprintf("%.2f", next)).Run(); err != nil {
			return strings.Join(saved, ";"), err
		}
		saved = append(saved, fmt.Sprintf("%s=%g", schema, cur))
	}
	if len(saved) == 0 {
		return "", errors.New("could not change the pointer speed")
	}
	return strings.Join(saved, ";"), nil
}

// RestorePointer puts back what AdjustPointer changed.
func RestorePointer(state string) error {
	var firstErr error
	for _, part := range strings.Split(state, ";") {
		schema, val, ok := strings.Cut(part, "=")
		if !ok || !strings.HasPrefix(schema, "org.gnome.desktop.peripherals.") {
			continue
		}
		if _, err := strconv.ParseFloat(val, 64); err != nil {
			continue
		}
		if err := exec.Command("gsettings", "set", schema, "speed", val).Run(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

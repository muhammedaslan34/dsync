package control

import (
	"errors"
	"strconv"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows pointer speed is 1..20 (10 is the default). It is changed for
// the session only, not saved, so signing out also restores it.
const (
	spiGetMouseSpeed = 0x0070
	spiSetMouseSpeed = 0x0071
	spifSendChange   = 0x02
)

var systemParametersInfo = windows.NewLazySystemDLL("user32.dll").NewProc("SystemParametersInfoW")

func getMouseSpeed() (int, error) {
	var speed uint32
	r, _, err := systemParametersInfo.Call(spiGetMouseSpeed, 0, uintptr(unsafe.Pointer(&speed)), 0)
	if r == 0 {
		return 0, err
	}
	return int(speed), nil
}

func setMouseSpeed(speed int) error {
	r, _, err := systemParametersInfo.Call(spiSetMouseSpeed, 0, uintptr(speed), spifSendChange)
	if r == 0 {
		return err
	}
	return nil
}

func PointerAdjustable() bool {
	_, err := getMouseSpeed()
	return err == nil
}

func AdjustPointer(step int) (string, error) {
	step = clampStep(step)
	if step == 0 {
		return "", nil
	}
	cur, err := getMouseSpeed()
	if err != nil {
		return "", err
	}
	if err := setMouseSpeed(max(1, min(20, cur+step*3))); err != nil {
		return "", err
	}
	return strconv.Itoa(cur), nil
}

func RestorePointer(state string) error {
	if state == "" {
		return nil
	}
	v, err := strconv.Atoi(state)
	if err != nil || v < 1 || v > 20 {
		return errors.New("bad saved pointer speed")
	}
	return setMouseSpeed(v)
}

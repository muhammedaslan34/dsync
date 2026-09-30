package control

// Pointer speed while controlling: dsync can make this computer's pointer
// faster or slower while Moonlight runs, and puts it back afterwards. In
// desktop mouse mode the remote pointer follows the local one, so this
// sets how fast it moves over there too.

// pointerSteps is how far each step of the slider moves the setting.
const maxPointerStep = 2

func clampStep(step int) int {
	return max(-maxPointerStep, min(step, maxPointerStep))
}

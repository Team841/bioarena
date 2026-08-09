//go:build !linux

package hardware

import "fmt"

// NewGpioFieldEStopPanel is not supported on non-Linux platforms.
// Use field_estop_no_pin: 0 in the event settings (or omit it) to use
// NoopFieldEStopPanel instead.
func NewGpioFieldEStopPanel(chip string, ncPin, noPin int) (*GpioFieldEStopPanel, error) {
	return nil, fmt.Errorf("GPIO field e-stop not supported on this platform (Linux/Raspberry Pi only)")
}

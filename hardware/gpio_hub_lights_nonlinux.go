//go:build !linux

package hardware

import "fmt"

// NewGpioHubLights is not supported on non-Linux platforms.
// Leave the hub lights trigger pin at 0 in the event settings to disable the driver.
func NewGpioHubLights(chip string, triggerPin, redWinPin, blueWinPin int) (*GpioHubLights, error) {
	return nil, fmt.Errorf("GPIO hub lights not supported on this platform (Linux/Raspberry Pi only)")
}

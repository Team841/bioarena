//go:build !linux

package hardware

import "fmt"

// NewGpioHubRelays is not supported on non-Linux platforms. Select the DMX output under
// Arena > Settings > LEDs to run without relays.
func NewGpioHubRelays(chip string, redPin, bluePin int, activeLow bool) (*GpioHubRelays, error) {
	return nil, fmt.Errorf("GPIO hub relays not supported on this platform (Linux/Raspberry Pi only)")
}

//go:build linux

package hardware

import (
	"fmt"
	"log"

	"github.com/warthog618/go-gpiocdev"
)

// NewGpioHubRelays opens the two relay lines on the named GPIO chip. chip is "gpiochip0" on
// Raspberry Pi; pins are BCM GPIO numbers from the event settings.
//
// Both lines are push-pull outputs requested dark. activeLow is for relay boards that switch
// on when their input is pulled LOW, as most opto-isolated modules do: the line is inverted
// in the kernel, so the driver's 1 still means lit.
func NewGpioHubRelays(chip string, redPin, bluePin int, activeLow bool) (*GpioHubRelays, error) {
	options := []gpiocdev.LineReqOption{gpiocdev.AsOutput(0)}
	if activeLow {
		options = append(options, gpiocdev.AsActiveLow)
	}

	red, err := gpiocdev.RequestLine(chip, redPin, options...)
	if err != nil {
		return nil, fmt.Errorf("open GPIO chip %q red relay pin %d: %w", chip, redPin, err)
	}
	blue, err := gpiocdev.RequestLine(chip, bluePin, options...)
	if err != nil {
		_ = red.Close()
		return nil, fmt.Errorf("open GPIO chip %q blue relay pin %d: %w", chip, bluePin, err)
	}

	polarity := "active-high"
	if activeLow {
		polarity = "active-low"
	}
	log.Printf("GpioHubRelays: opened %s red pin %d / blue pin %d (%s)", chip, redPin, bluePin, polarity)
	return newGpioHubRelays(red, blue), nil
}

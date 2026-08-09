//go:build linux

package hardware

import (
	"fmt"
	"log"

	"github.com/warthog618/go-gpiocdev"
)

// NewGpioFieldEStopPanel opens the named GPIO chip and pin pair.
// chip is "gpiochip0" on Raspberry Pi; pins are BCM GPIO numbers from the
// event settings. Both lines are configured as inputs with the Pi's internal
// pull-up enabled.
//
// ncPin = 0 opens a single-channel NO-only input, which cannot detect wiring
// faults; pass both pins to get fault detection.
func NewGpioFieldEStopPanel(chip string, ncPin, noPin int) (*GpioFieldEStopPanel, error) {
	noLine, err := gpiocdev.RequestLine(chip, noPin, gpiocdev.AsInput, gpiocdev.WithPullUp)
	if err != nil {
		return nil, fmt.Errorf("open GPIO chip %q NO pin %d: %w", chip, noPin, err)
	}
	panel := &GpioFieldEStopPanel{
		no:     noLine,
		filter: DiscrepancyFilter{Window: DefaultDiscrepancyWindow},
	}
	if ncPin == 0 {
		log.Printf("GpioFieldEStopPanel: opened %s NO pin %d (single-channel, no fault detection)", chip, noPin)
		return panel, nil
	}

	ncLine, err := gpiocdev.RequestLine(chip, ncPin, gpiocdev.AsInput, gpiocdev.WithPullUp)
	if err != nil {
		_ = noLine.Close()
		return nil, fmt.Errorf("open GPIO chip %q NC pin %d: %w", chip, ncPin, err)
	}
	panel.nc = ncLine
	log.Printf("GpioFieldEStopPanel: opened %s NC pin %d / NO pin %d (dual-channel)", chip, ncPin, noPin)
	return panel, nil
}

//go:build linux

package hardware

import (
	"fmt"
	"log"

	"github.com/warthog618/go-gpiocdev"
)

// NewGpioHubLights opens the trigger line and whichever winner lines are given on the
// named GPIO chip. chip is "gpiochip0" on Raspberry Pi; pins are BCM GPIO numbers from
// the event settings. redWinPin or blueWinPin at 0 leaves that switch input unwired;
// triggerPin is required, since without it there is nothing to drive.
//
// Every line is requested open-drain and active-low, released: the Arduino's pull-up
// holds it high until a SetValue(1) sinks it. The Pi's own bias is switched off, because
// the default on BCM 9-27 is a pull-down, and a released line with that left in place
// would sit at the divider between the two pull resistors -- about 2.9 V against a 5 V
// Arduino, right at its input threshold, and a phantom press waiting to happen.
func NewGpioHubLights(chip string, triggerPin, redWinPin, blueWinPin int) (*GpioHubLights, error) {
	if triggerPin == 0 {
		return nil, fmt.Errorf("hub lights: no trigger pin configured")
	}

	var opened []*gpiocdev.Line
	closeOpened := func() {
		for _, line := range opened {
			_ = line.Close()
		}
	}
	open := func(pin int, role string) (*gpiocdev.Line, error) {
		if pin == 0 {
			return nil, nil
		}
		line, err := gpiocdev.RequestLine(
			chip, pin, gpiocdev.AsOpenDrain, gpiocdev.AsActiveLow, gpiocdev.WithBiasDisabled, gpiocdev.AsOutput(0),
		)
		if err != nil {
			closeOpened()
			return nil, fmt.Errorf("open GPIO chip %q hub lights %s pin %d: %w", chip, role, pin, err)
		}
		opened = append(opened, line)
		return line, nil
	}

	trigger, err := open(triggerPin, "trigger")
	if err != nil {
		return nil, err
	}
	redWin, err := open(redWinPin, "red-wins")
	if err != nil {
		return nil, err
	}
	blueWin, err := open(blueWinPin, "blue-wins")
	if err != nil {
		return nil, err
	}

	// A nil *gpiocdev.Line must become a nil interface, or the driver would try to drive
	// a line that was never opened.
	var redWriter, blueWriter lineWriter
	if redWin != nil {
		redWriter = redWin
	}
	if blueWin != nil {
		blueWriter = blueWin
	}
	log.Printf(
		"GpioHubLights: opened %s trigger pin %d, red-wins pin %d, blue-wins pin %d (0 = unwired)",
		chip, triggerPin, redWinPin, blueWinPin,
	)
	return newGpioHubLights(trigger, redWriter, blueWriter, DefaultTriggerPulse), nil
}

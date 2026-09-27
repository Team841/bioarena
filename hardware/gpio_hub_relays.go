// Copyright 2026 10th Street Robotics. All Rights Reserved.
//
// GpioHubRelays is the practice-field alternative to DMX Hub lighting: one relay per HUB,
// each switching a plain red or blue lamp on the controller's own GPIO.
//
// It takes the same led.Mode sequence the DMX controller does, from the same place in the
// arena, and reduces each alliance's mode to on or off with led.Lit. So the relays follow
// upstream's lighting sequence exactly, down to which HUB is active when, and the only
// thing lost is what an on/off lamp cannot show.

package hardware

import (
	"fmt"
	"sync"

	"github.com/team841/bioarena/led"
)

// Default relay pins, BCM numbering: physical pins 15 and 16, beside a ground on 14.
//
// Chosen from BCM 9-27, which the Pi pulls down from power-on until bioarena claims them, so
// an active-high relay board stays dark through boot. A board that switches on LOW would
// light both lamps for that stretch; see docs/hardware-wiring.md for pins that suit one.
// Clear of the field e-stop pins in use or recommended (5, 6, 21, 24, 25, 26) and of the
// buses on 2-3, 7-11, 14-15 and 18-21.
const (
	DefaultHubRelayRedPin  = 22
	DefaultHubRelayBluePin = 23
)

// lineWriter abstracts a GPIO output for testability, the way lineReader does for the field
// e-stop. Values are logical: 1 lights the lamp and 0 darkens it, whichever level the relay
// board needs for that; the Linux constructor requests an active-low line when it needs LOW.
type lineWriter interface {
	SetValue(value int) error
}

// GpioHubRelays drives the two relays. Safe to call from any goroutine: the arena loop
// calls Update while the end-of-match sequence calls SetMode from a goroutine of its own.
type GpioHubRelays struct {
	red  lineWriter
	blue lineWriter

	mu       sync.Mutex
	redMode  led.Mode
	blueMode led.Mode
	driven   bool // whether redLit and blueLit are what the lines were last set to
	redLit   bool
	blueLit  bool
	closed   bool
}

// newGpioHubRelays wraps already-open lines. The Linux constructor and the tests come
// through here. Both lamps start dark, and Update drives the lines on its first call.
func newGpioHubRelays(red, blue lineWriter) *GpioHubRelays {
	return &GpioHubRelays{red: red, blue: blue, redMode: led.OffMode, blueMode: led.OffMode}
}

// SetMode records the mode for each HUB. Nothing is driven until Update.
func (relays *GpioHubRelays) SetMode(redMode, blueMode led.Mode) {
	relays.mu.Lock()
	defer relays.mu.Unlock()
	relays.redMode, relays.blueMode = redMode, blueMode
}

// GetModes returns the current mode for each HUB.
func (relays *GpioHubRelays) GetModes() (led.Mode, led.Mode) {
	relays.mu.Lock()
	defer relays.mu.Unlock()
	return relays.redMode, relays.blueMode
}

// Update drives the relays to match the current modes. Called at 100 Hz from the arena loop,
// so it only touches the lines when a lamp changes. A failed write is retried on the next
// call rather than assumed to have landed.
func (relays *GpioHubRelays) Update() error {
	relays.mu.Lock()
	defer relays.mu.Unlock()
	if relays.closed {
		return nil
	}
	redLit, blueLit := led.Lit(relays.redMode), led.Lit(relays.blueMode)
	if relays.driven && redLit == relays.redLit && blueLit == relays.blueLit {
		return nil
	}
	relays.driven = false
	if err := relays.red.SetValue(boolToValue(redLit)); err != nil {
		return fmt.Errorf("hub relays: red: %w", err)
	}
	if err := relays.blue.SetValue(boolToValue(blueLit)); err != nil {
		return fmt.Errorf("hub relays: blue: %w", err)
	}
	relays.redLit, relays.blueLit, relays.driven = redLit, blueLit, true
	return nil
}

// Close darkens both lamps and hands the lines back to the kernel.
//
// Required for the same reason the field e-stop has one: the output is rebuilt on every
// settings save, and the kernel gives a line to one requester at a time, so the replacement
// cannot open the pins until these are released. Idempotent.
func (relays *GpioHubRelays) Close() {
	relays.mu.Lock()
	defer relays.mu.Unlock()
	if relays.closed {
		return
	}
	relays.closed = true
	for _, line := range []lineWriter{relays.red, relays.blue} {
		_ = line.SetValue(0)
		if closer, ok := line.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	}
}

func boolToValue(lit bool) int {
	if lit {
		return 1
	}
	return 0
}

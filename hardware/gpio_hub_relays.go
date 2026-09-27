// Copyright 2026 10th Street Robotics. All Rights Reserved.
//
// GpioHubRelays is the practice-field alternative to DMX Hub lighting: one relay per HUB,
// each switching a plain red or blue lamp on the controller's own GPIO.
//
// It takes the same led.Mode sequence the DMX controller does, from the same place in the
// arena, and turns each alliance's mode into on, off or a blink with led.Lit. So the relays
// follow upstream's lighting sequence exactly: which HUB is active, the warning before one
// goes inactive, and which one goes inactive first. What they lose is only colour and shape.

package hardware

import (
	"fmt"
	"sync"
	"time"

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
	mu     sync.Mutex
	red    relayLamp
	blue   relayLamp
	now    func() time.Time // the clock blinks are timed against; replaced in tests
	closed bool
}

// relayLamp is one HUB's relay and what it was last told and last driven to.
type relayLamp struct {
	name   string
	line   lineWriter
	mode   led.Mode
	since  time.Time // when mode last changed, which is where a blink is timed from
	lit    bool      // what the line was last set to, once driven is true
	driven bool
}

// newGpioHubRelays wraps already-open lines. The Linux constructor and the tests come
// through here. Both lamps start dark, and Update drives the lines on its first call.
func newGpioHubRelays(red, blue lineWriter) *GpioHubRelays {
	return &GpioHubRelays{
		red:  relayLamp{name: "red", line: red, mode: led.OffMode},
		blue: relayLamp{name: "blue", line: blue, mode: led.OffMode},
		now:  time.Now,
	}
}

// SetMode records the mode for each HUB. Nothing is driven until Update.
//
// The arena sets the modes on every tick, so a blink is timed from when a mode actually
// changed rather than from the last call -- the same reset upstream's zones make.
func (relays *GpioHubRelays) SetMode(redMode, blueMode led.Mode) {
	relays.mu.Lock()
	defer relays.mu.Unlock()
	now := relays.now()
	relays.red.setMode(redMode, now)
	relays.blue.setMode(blueMode, now)
}

func (lamp *relayLamp) setMode(mode led.Mode, now time.Time) {
	if mode != lamp.mode {
		lamp.mode, lamp.since = mode, now
	}
}

// GetModes returns the current mode for each HUB.
func (relays *GpioHubRelays) GetModes() (led.Mode, led.Mode) {
	relays.mu.Lock()
	defer relays.mu.Unlock()
	return relays.red.mode, relays.blue.mode
}

// Update drives the relays to match the current modes. Called at 100 Hz from the arena loop,
// so a line is only written when its lamp changes: once per half-period while blinking,
// otherwise once per mode change. A failed write is retried on the next call rather than
// assumed to have landed.
func (relays *GpioHubRelays) Update() error {
	relays.mu.Lock()
	defer relays.mu.Unlock()
	if relays.closed {
		return nil
	}
	now := relays.now()
	if err := relays.red.drive(now); err != nil {
		return err
	}
	return relays.blue.drive(now)
}

func (lamp *relayLamp) drive(now time.Time) error {
	lit := led.Lit(lamp.mode, now.Sub(lamp.since))
	if lamp.driven && lit == lamp.lit {
		return nil
	}
	lamp.driven = false
	if err := lamp.line.SetValue(boolToValue(lit)); err != nil {
		return fmt.Errorf("hub relays: %s: %w", lamp.name, err)
	}
	lamp.lit, lamp.driven = lit, true
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
	for _, line := range []lineWriter{relays.red.line, relays.blue.line} {
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

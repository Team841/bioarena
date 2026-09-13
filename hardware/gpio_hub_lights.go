// Copyright 2026 10th Street Robotics. All Rights Reserved.
//
// GpioHubLights drives the practice-field hub lights through the Arduino that already
// runs them (HighlanderRobotics/rebuilt-hub-lights). That sketch keeps its own 2:43 match
// clock and renders both hubs itself; its only inputs are a momentary start/stop button
// and a three-position auto-winner toggle, each an INPUT_PULLUP pin that a contact pulls
// to ground. This driver stands in for the operator's hand on both.
//
// Wiring: three Raspberry Pi GPIO lines run to the Arduino's button and switch inputs,
// with a common ground between the two boards. Each line is driven open-drain: pulled
// to ground to assert, left floating to release, so the Arduino's own 5 V pull-up sets
// the idle level and the Pi never sources 3.3 V into a 5 V input. The physical button
// and toggle can stay wired in parallel; a floating Pi line is invisible to them.
//
//   - Trigger  -> Arduino pin 2 (BUTTON_PIN). Pulsed once at match start, and once more
//     on an abort. The sketch toggles: a press from IDLE or MATCH_OVER starts its match, a
//     press from anywhere else stops it. A match that runs to the end is left alone --
//     the Arduino's clock ends it, and a press then would start another.
//   - Red wins -> Arduino pin 4 (SWITCH_RED). Held asserted from match start while red
//     won AUTO.
//   - Blue wins -> Arduino pin 5 (SWITCH_BLUE). Likewise for blue.
//
// The sketch samples the switch pins once, on entering TRANSITION about 23 s in, and
// treats neither-asserted as red. So the winner lines are set together with the start
// press and held until the match ends, and a field wired with only the blue line still
// gets both outcomes. The physical toggle must sit in its centre position: a toggle left
// on one side asserts that pin alongside ours, and both-asserted also reads as red.
//
// One thing this cannot do is keep the two clocks aligned. The Arduino runs 10.5 s
// transition and 25.4 s shifts against bioarena's 10 and 25, so the hubs drift a second
// or two behind the field by endgame. Bring the sketch's constants into line if that
// matters.

package hardware

import (
	"fmt"
	"log"
	"sync"
	"time"
)

// DefaultTriggerPulse is how long the trigger line is held asserted per press, and the
// gap kept before the next one. The sketch debounces at 80 ms and renders 1280 pixels
// between reads, so a press has to outlast a full loop iteration to be seen at all, and
// two presses need a gap between them to be seen as two.
const DefaultTriggerPulse = 250 * time.Millisecond

// lineWriter abstracts a GPIO output for testability, the way lineReader does for the
// field e-stop. Values are logical: 1 asserts the line (pulled to ground), 0 releases
// it. The production constructor supplies a *gpiocdev.Line requested active-low and
// open-drain so those are the values it takes directly.
type lineWriter interface {
	SetValue(value int) error
}

// GpioHubLights implements FieldLights by pressing the Arduino's inputs.
// It is safe to call SetState and Close from any goroutine.
type GpioHubLights struct {
	trigger lineWriter
	redWin  lineWriter // nil when not wired
	blueWin lineWriter // nil when not wired
	pulse   time.Duration

	// presses hands each press to a goroutine of its own, because a press is a pulse
	// with a sleep in the middle and SetState is called from the arena loop under its
	// lock. done closes when that goroutine has exited.
	presses chan struct{}
	done    chan struct{}

	mu      sync.Mutex
	running bool // a press has started the Arduino's match and nothing has ended it
	closed  bool
}

// Compile-time interface assertion.
var _ FieldLights = (*GpioHubLights)(nil)

// newGpioHubLights assembles a driver over already-open lines and starts its presser.
// redWin and blueWin may be nil. The Linux constructor and the tests both come through
// here; nothing else should.
func newGpioHubLights(trigger, redWin, blueWin lineWriter, pulse time.Duration) *GpioHubLights {
	h := &GpioHubLights{
		trigger: trigger,
		redWin:  redWin,
		blueWin: blueWin,
		pulse:   pulse,
		// Two deep: a start and the abort that follows it is the most anyone can queue
		// before the first has finished.
		presses: make(chan struct{}, 2),
		done:    make(chan struct{}),
	}
	go h.pressLoop()
	return h
}

// SetState presses the button and sets the switch as the match moves.
//
// Only three transitions matter: into AUTO, which starts the Arduino's match; into
// Finished by an abort, which stops it; and into Finished by the clock, which the Arduino
// reaches on its own and must not be pressed for. Everything in between -- shifts,
// warnings, the pause -- is the Arduino's own business.
func (h *GpioHubLights) SetState(state LightingState) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return fmt.Errorf("hub lights: driver is closed")
	}

	switch {
	case state.Phase == PhaseAuto && !h.running:
		// Winner first, so it is settled before the sketch could possibly sample it.
		if err := h.setWinner(state.AutoWinner); err != nil {
			return err
		}
		h.press()
		h.running = true

	case state.Phase == PhaseFinished && h.running:
		h.running = false
		if state.Aborted {
			h.press()
		}
		return h.setWinner(AllianceNone)
	}
	return nil
}

// press queues one pulse of the trigger line. It never blocks the caller: the arena
// loop is on the other side of this, and a press that cannot be queued is logged and
// dropped rather than waited for.
func (h *GpioHubLights) press() {
	select {
	case h.presses <- struct{}{}:
	default:
		log.Println("GpioHubLights: press dropped, previous presses still in flight")
	}
}

// setWinner drives the switch lines for the given AUTO result. Lines that are not wired
// are skipped; AllianceNone releases both.
func (h *GpioHubLights) setWinner(winner Alliance) error {
	drive := func(line lineWriter, asserted bool) error {
		if line == nil {
			return nil
		}
		value := 0
		if asserted {
			value = 1
		}
		return line.SetValue(value)
	}
	if err := drive(h.redWin, winner == AllianceRed); err != nil {
		return fmt.Errorf("hub lights: set red-wins line: %w", err)
	}
	if err := drive(h.blueWin, winner == AllianceBlue); err != nil {
		return fmt.Errorf("hub lights: set blue-wins line: %w", err)
	}
	return nil
}

// pressLoop performs queued presses one at a time: assert, hold, release, and then hold
// off for as long again so the next press is a separate edge.
func (h *GpioHubLights) pressLoop() {
	defer close(h.done)
	for range h.presses {
		if err := h.trigger.SetValue(1); err != nil {
			log.Printf("GpioHubLights: assert trigger: %v", err)
			continue
		}
		time.Sleep(h.pulse)
		if err := h.trigger.SetValue(0); err != nil {
			log.Printf("GpioHubLights: release trigger: %v", err)
		}
		time.Sleep(h.pulse)
	}
}

// Close releases every line, floating, and returns them to the kernel.
//
// Required for the same reason the field e-stop has one: the driver is rebuilt whenever
// settings are saved, and the kernel gives a line to one requester at a time. A press in
// flight is allowed to finish first, so the Arduino is never left with the button held.
// Idempotent.
func (h *GpioHubLights) Close() {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.closed = true
	close(h.presses)
	h.mu.Unlock()
	<-h.done

	for _, line := range []lineWriter{h.trigger, h.redWin, h.blueWin} {
		if line == nil {
			continue
		}
		_ = line.SetValue(0)
		if closer, ok := line.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	}
}

// Copyright 2026 Team 254. All Rights Reserved.
// Portions Copyright Team 841. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)
//
// Game-specific control of the 2026 Hub DMX lighting.
//
// Ported from upstream cheesy-arena's field/arena_leds.go. The mode selection is
// unchanged; the only adaptation is the source of the AUTO result. Upstream reads
// WonAuto off each alliance's realtime score, which bioarena does not have, so the
// Hubs are constructed from arena.AutoWinner instead.

package field

import (
	"log"
	"time"

	"github.com/team841/bioarena/game"
	"github.com/team841/bioarena/hardware"
	"github.com/team841/bioarena/led"
	"github.com/team841/bioarena/model"
)

const (
	hubLightWarningSec           = 3
	hubLightScoringAssessmentSec = 3
)

// ledController is the Hub lighting output. Three implementations behind one sequence:
// led.Controller speaks E1.31 sACN and led.ArtNetController Art-Net, both to upstream's
// full-field fixtures, and hardware.GpioHubRelays switches one lamp per HUB. The mode
// selection below is upstream's and never knows which it is driving.
type ledController interface {
	SetMode(redMode, blueMode led.Mode)
	GetModes() (led.Mode, led.Mode)
	Update() error
}

// applyHubLedSettings rebuilds the lighting output from the stored settings. Called on
// every settings load, so a change takes effect without restarting bioarena.
//
// Rebuilt every time rather than adjusted, because every output holds something that has to
// be let go of first: the DMX socket, or the relay GPIO lines, which the kernel will not
// hand to the replacement while the old output still has them. Settings cannot be saved
// mid-match, so nothing is lost by starting from Off.
func (arena *Arena) applyHubLedSettings(settings *model.EventSettings) {
	// A settings save is a fresh chance to report, so a failure that persists across one is
	// said again rather than staying suppressed from before the change.
	arena.hubLedsFailing = false

	if arena.Leds != nil {
		releaseHubLeds(arena.Leds)
	}
	arena.Leds = newHubLeds(settings)
}

// newHubLeds builds the configured output. A problem is logged rather than returned: a
// relay pin that will not open, or an address that will not resolve, should not stop the
// arena from loading. Either way the sequences still run, with nothing sent.
func newHubLeds(settings *model.EventSettings) ledController {
	if settings.HubRelays {
		relays, err := hardware.NewGpioHubRelays(
			"gpiochip0", settings.HubRelayRedPin, settings.HubRelayBluePin, settings.HubRelayActiveLow,
		)
		if err != nil {
			log.Printf(
				"WARNING: Could not open hub relay GPIO pins (red %d, blue %d): %v",
				settings.HubRelayRedPin, settings.HubRelayBluePin, err,
			)
			return led.NewController()
		}
		return relays
	}

	var dmx interface {
		ledController
		SetAddress(address string) error
	}
	protocol := "sACN"
	if settings.HubLedsArtNet {
		dmx, protocol = led.NewArtNetController(), "Art-Net"
	} else {
		dmx = led.NewController()
	}
	if settings.HubLedsAddress == "" {
		log.Println("WARNING: No Hub LED address configured — Hub LED output disabled.")
	} else if err := dmx.SetAddress(settings.HubLedsAddress); err != nil {
		log.Printf("Hub LEDs: could not set address %q: %v", settings.HubLedsAddress, err)
	} else {
		log.Printf("Hub LEDs: sending %s to %s.", protocol, settings.HubLedsAddress)
	}
	return dmx
}

// releaseHubLeds lets go of what an outgoing output holds. The relays have a Close; the DMX
// controllers close their socket on a blank address, because the led package is kept
// byte-identical to upstream and has no Close to add -- see docs/upstream-divergences.md.
func releaseHubLeds(leds ledController) {
	switch leds := leds.(type) {
	case interface{ Close() }:
		leds.Close()
	case interface{ SetAddress(string) error }:
		_ = leds.SetAddress("")
	}
}

// hubs lives in arena_hub_counter.go, alongside the FUEL counting that also needs them.

// redWonAuto reports whether the red alliance is the AUTO winner.
func (arena *Arena) redWonAuto() bool {
	return arena.AutoWinner == hardware.AllianceRed
}

// updateHubLeds updates Hub LEDs based on the current match state and active scoring shift.
func (arena *Arena) updateHubLeds(currentTime time.Time) {
	switch arena.MatchState {
	case AutoPeriod:
		arena.Leds.SetMode(led.RedStartupMode, led.BlueStartupMode)
	case PausePeriod:
		arena.Leds.SetMode(led.RedMode, led.BlueMode)
	case TeleopPeriod:
		arena.updateTeleopHubLeds(currentTime)
	case PostMatch:
		if arena.lastMatchState != PostMatch {
			// Set the Hub LEDs to white at the end of the match, and then turn them off when the referees are supposed
			// to assess tower climbs.
			arena.Leds.SetMode(led.WhiteMode, led.WhiteMode)
			go func() {
				time.Sleep(hubLightScoringAssessmentSec * time.Second)
				arena.Leds.SetMode(led.OffMode, led.OffMode)
			}()
		}
	}

	// Logged on the transition into failure and on recovery, not on every attempt. This runs
	// on the arena loop at 100 Hz, so an unreachable receiver -- a gateway that is unplugged,
	// or an address left pointing at hardware that is not there -- otherwise writes a hundred
	// identical lines a second and buries everything else in the journal.
	if err := arena.Leds.Update(); err != nil {
		if !arena.hubLedsFailing {
			arena.hubLedsFailing = true
			log.Printf("Failed to update hub LEDs: %s", err)
			log.Println(
				"Hub LEDs: suppressing further failures until they recover. " +
					"Change the output under Arena > Settings > LEDs to stop trying.",
			)
		}
	} else if arena.hubLedsFailing {
		arena.hubLedsFailing = false
		log.Println("Hub LEDs: updating again.")
	}
}

// updateTeleopHubLeds updates teleop LED modes using the active Hub shift, auto winner, and warning window.
func (arena *Arena) updateTeleopHubLeds(currentTime time.Time) {
	redHub, blueHub := arena.hubs()

	shift, remaining, _, ok := redHub.GetCurrentShiftTiming(arena.MatchStartTime, currentTime)
	if !ok {
		return
	}

	redRemaining, _ := redHub.GetActiveShiftTiming(arena.MatchStartTime, currentTime)
	blueRemaining, _ := blueHub.GetActiveShiftTiming(arena.MatchStartTime, currentTime)

	redMode := led.OffMode
	if redRemaining > 0 {
		redMode = led.RedMode
	}
	blueMode := led.OffMode
	if blueRemaining > 0 {
		blueMode = led.BlueMode
	}

	// Pulse the LEDs when the Hub is about to go inactive.
	if remaining <= time.Duration(hubLightWarningSec)*time.Second {
		switch shift {
		case game.ShiftTransition:
			if arena.redWonAuto() {
				redMode = led.RedPulseMode
			} else {
				blueMode = led.BluePulseMode
			}
		case game.Shift1, game.Shift3:
			if arena.redWonAuto() {
				blueMode = led.BluePulseMode
			} else {
				redMode = led.RedPulseMode
			}
		case game.Shift2:
			if arena.redWonAuto() {
				redMode = led.RedPulseMode
			} else {
				blueMode = led.BluePulseMode
			}
		case game.ShiftEndgame:
			redMode = led.RedPulseMode
			blueMode = led.BluePulseMode
		default:
		}
	} else if shift == game.ShiftTransition {
		if arena.redWonAuto() {
			redMode = led.RedAdvantageMode
		} else {
			blueMode = led.BlueAdvantageMode
		}
	}
	arena.Leds.SetMode(redMode, blueMode)
}

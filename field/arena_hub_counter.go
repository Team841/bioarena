// Portions Copyright Team 841. All Rights Reserved.
//
// Wiring for the Hub FUEL counter feed.
//
// The counter substitutes for upstream's PLC Hub counters, so the scoring itself is
// upstream's two lines and lives in updateHubCounts. What is here besides that is the
// translation between arena state and the counter's own vocabulary, which is a wire
// format of docs/spec-hub-fuel-counter-feed.md rather than anything the arena knows.

package field

import (
	"log"
	"time"

	"github.com/team841/bioarena/game"
	"github.com/team841/bioarena/hardware"
	"github.com/team841/bioarena/model"
	"github.com/team841/bioarena/plc"
)

// applyHubCounterSettings rebuilds the counter receiver from the stored settings, and
// wraps it into arena.Plc so the scoring path reads it where upstream reads the PLC.
//
// Called on every settings load, so a change takes effect without a restart. The old
// receiver is closed first: it holds the UDP port, and the replacement cannot bind while
// it does.
//
// A failure is logged rather than returned. A counter that will not start must not stop
// the arena from loading -- scoring is not a safety function -- and the operator sees it
// as NOT CONFIGURED on the match panel, which blocks a Counted-mode match and nothing
// else.
func (arena *Arena) applyHubCounterSettings(settings *model.EventSettings) {
	if arena.hubCounter != nil {
		arena.hubCounter.Close()
		arena.hubCounter = nil
	}
	// Unwrap first, so a field that had a counter and no longer does goes back to the
	// plain PLC rather than keeping a wrapper around a closed receiver.
	arena.Plc = plc.WithHubCounts(arena.Plc, nil)

	if settings.HubCounterAddress == "" {
		return
	}
	port := settings.HubCounterPort
	if port == 0 {
		port = hardware.DefaultHubCounterPort
	}

	receiver, err := hardware.NewHubCounterReceiver(settings.HubCounterAddress, port)
	if err != nil {
		log.Printf("WARNING: Could not start the Hub FUEL counter: %v", err)
		return
	}
	arena.hubCounter = receiver
	arena.Plc = plc.WithHubCounts(arena.Plc, receiver)
	go receiver.Run()
}

// HubCounterStatus is what the match panel says about the feed: ONLINE, OFFLINE, or
// NOT CONFIGURED when there is no counter at all.
func (arena *Arena) HubCounterStatus() string {
	if arena.hubCounter == nil {
		return HubCounterNotConfigured
	}
	return arena.hubCounter.Status()
}

// HubCounterNotConfigured is the arena's own status: the absence of a receiver rather
// than a state one can be in, which is why it is not in hardware/ with the other two.
const HubCounterNotConfigured = "NOT CONFIGURED"

// resetHubs zeroes the per-shift FUEL tallies.
//
// Called when a match is loaded and again when one starts. Upstream only needs the
// former, because it loads a match immediately before running it. On a practice field the
// driver shoots in PreMatch for ten minutes first, and the counts that accumulate have to
// go before the match they are not part of.
func (arena *Arena) resetHubs() {
	arena.RedHub = game.Hub{}
	arena.BlueHub = game.Hub{}
}

// hubs returns the two Hubs with WonAuto set from the AUTO result.
//
// Derived here on every call rather than assigned when the winner is decided, so
// arena.AutoWinner stays the single source of truth for it and the two cannot drift.
// Before the decision both read false, which is symmetric and harmless: the shifts whose
// activity depends on WonAuto are all in teleop, after it.
func (arena *Arena) hubs() (*game.Hub, *game.Hub) {
	arena.RedHub.WonAuto = arena.AutoWinner == hardware.AllianceRed
	arena.BlueHub.WonAuto = arena.AutoWinner == hardware.AllianceBlue
	return &arena.RedHub, &arena.BlueHub
}

// updateHubCounts folds the counter's totals into the Hubs, and publishes the match
// context the counter's status reply is built from.
//
// The two scoring lines are upstream's, out of handlePlcInputOutput. They run from Update
// instead because that returns early when the PLC is not enabled, and on a practice field
// it never is.
func (arena *Arena) updateHubCounts() {
	redHub, blueHub := arena.hubs()

	currentTime := time.Now()
	redHubCount, blueHubCount := arena.Plc.GetHubCounts()
	redHub.UpdateState(redHubCount, arena.MatchStartTime, currentTime)
	blueHub.UpdateState(blueHubCount, arena.MatchStartTime, currentTime)

	if arena.hubCounter != nil {
		arena.hubCounter.SetMatchContext(arena.hubMatchContext(currentTime))
	}
}

// hubMatchContext describes the match in the counter protocol's terms, so the receiver
// can answer a status datagram without reading arena state from its own goroutine.
func (arena *Arena) hubMatchContext(currentTime time.Time) hardware.HubMatchContext {
	redHub, blueHub := arena.hubs()

	shift, _, _, inShift := redHub.GetCurrentShiftTiming(arena.MatchStartTime, currentTime)
	shiftName := "NONE"
	if inShift {
		shiftName = hubShiftName(shift)
	}

	return hardware.HubMatchContext{
		MatchState:   hubMatchStateName(arena.MatchState),
		MatchTimeSec: arena.MatchTimeSec(),
		Shift:        shiftName,
		RedActive:    inShift && redHub.IsShiftActive(shift),
		BlueActive:   inShift && blueHub.IsShiftActive(shift),
		RedCount:     hubTotalCount(redHub),
		BlueCount:    hubTotalCount(blueHub),
		RedCredited:  hubCreditedCount(redHub),
		BlueCredited: hubCreditedCount(blueHub),
		RedAuto:      redHub.GetShiftCount(game.ShiftAuto, true),
		BlueAuto:     blueHub.GetShiftCount(game.ShiftAuto, true),
	}
}

// hubTotalCount is every FUEL the camera saw enter this HUB this match, active or not.
func hubTotalCount(hub *game.Hub) int {
	var count int
	for shift := game.ShiftAuto; shift < game.ShiftCount; shift++ {
		count += hub.GetShiftCount(shift, false)
	}
	return count
}

// hubCreditedCount is the FUEL that scored: the subset that entered while the HUB was
// active. The difference between this and hubTotalCount is what was shot into a dark HUB.
func hubCreditedCount(hub *game.Hub) int {
	var count int
	for shift := game.ShiftAuto; shift < game.ShiftCount; shift++ {
		count += hub.GetShiftCount(shift, true)
	}
	return count
}

// hubShiftName is the shift's name in the counter protocol. Spelled out rather than
// generated, because these are wire values: a stringer over game.Shift would rename them
// the day upstream renames a constant.
func hubShiftName(shift game.Shift) string {
	switch shift {
	case game.ShiftAuto:
		return "AUTO"
	case game.ShiftTransition:
		return "TRANSITION"
	case game.Shift1:
		return "SHIFT1"
	case game.Shift2:
		return "SHIFT2"
	case game.Shift3:
		return "SHIFT3"
	case game.Shift4:
		return "SHIFT4"
	case game.ShiftEndgame:
		return "ENDGAME"
	case game.ShiftPostMatch:
		return "POST_MATCH"
	default:
		return "NONE"
	}
}

// hubMatchStateName is the match state's name in the counter protocol. The same names the
// web UI uses in static/js/match_timing.js.
func hubMatchStateName(state MatchState) string {
	switch state {
	case PreMatch:
		return "PRE_MATCH"
	case StartMatch:
		return "START_MATCH"
	case WarmupPeriod:
		return "WARMUP_PERIOD"
	case AutoPeriod:
		return "AUTO_PERIOD"
	case PausePeriod:
		return "PAUSE_PERIOD"
	case TeleopPeriod:
		return "TELEOP_PERIOD"
	case PostMatch:
		return "POST_MATCH"
	default:
		return "UNKNOWN"
	}
}

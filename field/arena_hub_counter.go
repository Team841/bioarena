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
	"fmt"
	"log"
	"strings"
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

// checkHubCounterReady blocks a match start that the AUTO result would be decided wrongly
// by. Only in Counted mode: in the other three the counter is display-only, and a field
// whose camera is down should still be able to run practice matches.
//
// Starting anyway would not fail loudly. Both HUBs would count nothing, the tie-break
// would flip a coin, and the match would run to completion looking exactly like a correct
// one -- with the driver's AUTO having had no bearing on the advantage they got.
func (arena *Arena) checkHubCounterReady() error {
	if arena.AutoWinnerMode != AutoWinnerCounted {
		return nil
	}
	if arena.hubCounter == nil {
		return fmt.Errorf(
			"cannot start match: AUTO winner is set to Counted but no Hub FUEL counter is " +
				"configured (Setup > Settings)",
		)
	}
	if status := arena.hubCounter.Status(); status != hardware.HubCounterOnline {
		return fmt.Errorf(
			"cannot start match: AUTO winner is set to Counted but the Hub FUEL counter is %s",
			status,
		)
	}
	return nil
}

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
	return &arena.RedHub, &arena.BlueHub
}

// syncHubWonAuto copies the AUTO result onto the Hubs, which is what their shift
// alternation keys on.
//
// Kept separate from hubs() and called only from the arena goroutine. hubs() has to stay
// a pure read because the notifier message generators call it, and websocket.go runs those
// on a web goroutine with no arena lock whenever a client subscribes -- a writer there
// races the match loop on every page load.
func (arena *Arena) syncHubWonAuto() {
	arena.RedHub.WonAuto = arena.AutoWinner == hardware.AllianceRed
	arena.BlueHub.WonAuto = arena.AutoWinner == hardware.AllianceBlue
}

// updateHubCounts folds the counter's totals into the Hubs, and publishes the match
// context the counter's status reply is built from.
//
// The two scoring lines are upstream's, out of handlePlcInputOutput. They run from Update
// instead because that returns early when the PLC is not enabled, and on a practice field
// it never is.
func (arena *Arena) updateHubCounts() {
	// The one place the Hubs' WonAuto is written, on the arena goroutine under the lock.
	arena.syncHubWonAuto()
	redHub, blueHub := arena.hubs()

	currentTime := time.Now()
	redHubCount, blueHubCount := arena.Plc.GetHubCounts()
	redHub.UpdateState(redHubCount, arena.MatchStartTime, currentTime)
	blueHub.UpdateState(blueHubCount, arena.MatchStartTime, currentTime)

	if arena.hubCounter != nil {
		arena.hubCounter.SetMatchContext(arena.hubMatchContext(currentTime))
	}

	arena.notifyHubScoreIfChanged()
}

// activeHubs reports the shift a FUEL arriving now would be credited to, and which HUBs
// would credit it.
//
// The scoring clock, not the lighting one: through the auto/teleop pause this still says
// AUTO with both HUBs active, which is the truth a counter and an operator both need --
// it is the window the AUTO result is still being settled in. Shared by the counter's
// status reply and the match panel's banner so the two cannot disagree.
func (arena *Arena) activeHubs(currentTime time.Time) (shiftName string, red, blue, inShift bool) {
	redHub, blueHub := arena.hubs()

	shift, ok := redHub.ScoringShift(arena.MatchStartTime, currentTime)
	if !ok {
		return "NONE", false, false, false
	}
	return hubShiftName(shift), redHub.IsShiftActive(shift), blueHub.IsShiftActive(shift), true
}

// hubMatchContext describes the match in the counter protocol's terms, so the receiver
// can answer a status datagram without reading arena state from its own goroutine.
func (arena *Arena) hubMatchContext(currentTime time.Time) hardware.HubMatchContext {
	redHub, blueHub := arena.hubs()
	shiftName, redActive, blueActive, _ := arena.activeHubs(currentTime)

	return hardware.HubMatchContext{
		MatchState:   hubMatchStateName(arena.MatchState),
		MatchTimeSec: arena.MatchTimeSec(),
		Shift:        shiftName,
		RedActive:    redActive,
		BlueActive:   blueActive,
		RedCount:     hubTotalCount(redHub),
		BlueCount:    hubTotalCount(blueHub),
		RedCredited:  hubCreditedCount(redHub),
		BlueCredited: hubCreditedCount(blueHub),
		RedAuto:      redHub.GetShiftCount(game.ShiftAuto, true),
		BlueAuto:     blueHub.GetShiftCount(game.ShiftAuto, true),
	}
}

// hubScoreView is one alliance's tallies, as the match panel reads them.
type hubScoreView struct {
	// Counted is every FUEL the camera saw enter this HUB; Credited is the subset that
	// entered while it was active, which is the score. The difference is what was shot
	// into a dark HUB.
	Counted  int
	Credited int
	Auto     int
	// ByShift is the per-shift breakdown, counted rather than credited, indexed by
	// game.Shift. The whole match's shape in one array, for a panel that wants it.
	ByShift []int
	// NoTeam is set when this HUB has been credited FUEL but no team is registered on its
	// alliance -- a driver station plugged into the wrong side, or a camera with the hubs
	// swapped. Worth saying out loud, because the scoring looks fine until someone checks
	// which alliance got it.
	NoTeam bool
}

// generateHubScoreMessage is the hubScore notifier's payload.
func (arena *Arena) generateHubScoreMessage() any {
	redHub, blueHub := arena.hubs()
	ageMs, info, session := 0, "", ""
	if arena.hubCounter != nil {
		ageMs, info, session = arena.hubCounter.Telemetry()
	}

	return &struct {
		Counter struct {
			Status    string
			LatencyMs int
			Info      string
			Session   string
		}
		Red  hubScoreView
		Blue hubScoreView
	}{
		Counter: struct {
			Status    string
			LatencyMs int
			Info      string
			Session   string
		}{arena.HubCounterStatus(), ageMs, info, session},
		Red:  arena.hubScoreView(redHub, "R"),
		Blue: arena.hubScoreView(blueHub, "B"),
	}
}

// hubScoreView projects one Hub, with the wrong-side warning for its alliance.
func (arena *Arena) hubScoreView(hub *game.Hub, stationPrefix string) hubScoreView {
	byShift := make([]int, game.ShiftCount)
	for shift := game.ShiftAuto; shift < game.ShiftCount; shift++ {
		byShift[shift] = hub.GetShiftCount(shift, false)
	}
	counted := hubTotalCount(hub)
	return hubScoreView{
		Counted:  counted,
		Credited: hubCreditedCount(hub),
		Auto:     hub.GetShiftCount(game.ShiftAuto, true),
		ByShift:  byShift,
		// Counted, not credited. The mistake being reported is that the camera saw FUEL
		// enter this HUB while nobody is registered on its alliance -- a driver station on
		// the wrong side, or a camera with the hubs swapped. Whether those balls scored is
		// beside the point, and keying on credited would hide the warning for the whole of
		// whichever shifts this HUB is dark, which is half of teleop.
		NoTeam: counted > 0 && !arena.allianceHasTeam(stationPrefix),
	}
}

// allianceHasTeam reports whether any station on the alliance has a team registered.
func (arena *Arena) allianceHasTeam(stationPrefix string) bool {
	for station, allianceStation := range arena.AllianceStations {
		if strings.HasPrefix(station, stationPrefix) && allianceStation.Team != nil {
			return true
		}
	}
	return false
}

// hubScoreSnapshot is what a change is detected against. Only the values the panel shows:
// a tally that has not moved and a feed that has not changed state should not wake every
// kiosk up.
type hubScoreSnapshot struct {
	redCounted, redCredited   int
	blueCounted, blueCredited int
	counterStatus             string
	latencyMs                 int
}

// notifyHubScoreIfChanged fires the hubScore notifier when anything on the panel moved.
func (arena *Arena) notifyHubScoreIfChanged() {
	redHub, blueHub := arena.hubs()
	latencyMs := 0
	if arena.hubCounter != nil {
		latencyMs, _, _ = arena.hubCounter.Telemetry()
	}
	snapshot := hubScoreSnapshot{
		redCounted:    hubTotalCount(redHub),
		redCredited:   hubCreditedCount(redHub),
		blueCounted:   hubTotalCount(blueHub),
		blueCredited:  hubCreditedCount(blueHub),
		counterStatus: arena.HubCounterStatus(),
		latencyMs:     latencyMs,
	}
	if snapshot == arena.lastHubScore {
		return
	}
	arena.lastHubScore = snapshot
	arena.HubScoreNotifier.Notify()
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

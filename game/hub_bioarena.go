// Portions Copyright Team 841. All Rights Reserved.
//
// Fork-local additions to the 2026 Hub.
//
// Separate from hub.go so that file can be taken from upstream byte-identically -- the
// shift alternation and the FUEL attribution are rules of the game, and a fork that
// edits them in place is a fork that drifts away from them. See
// docs/upstream-divergences.md.

package game

import "time"

// IsShiftActive reports whether the Hub is lit for the given shift.
//
// Exported only because bioarena's lighting drivers live outside this package, where
// upstream's callers are all inside it. The rule is upstream's isShiftActive, untouched.
func (hub *Hub) IsShiftActive(shift Shift) bool {
	return hub.isShiftActive(shift)
}

// ScoringShift returns the shift a FUEL arriving now would be credited to, and whether
// there is one.
//
// Not the same as GetCurrentShiftTiming, and the difference is the point. That one is the
// lighting clock: it reports no shift during the auto/teleop pause and once teleop has
// ended. This one carries the scoring grace periods, so it still says AUTO through the
// whole pause -- which is why a ball shot at the AUTO buzzer counts toward the AUTO
// result, settled at the end of that pause.
//
// Exported for the same reason as IsShiftActive: bioarena has to answer "would this score
// right now" from outside this package, for the counter's status reply and the field
// banner. The rule is upstream's getCurrentShift, untouched.
func (hub *Hub) ScoringShift(matchStartTime, currentTime time.Time) (Shift, bool) {
	return hub.getCurrentShift(matchStartTime, currentTime)
}

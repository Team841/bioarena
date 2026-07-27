// Copyright 2026 Team 254. All Rights Reserved.
// Portions Copyright Team 841. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)
//
// Shift structure for the 2026 Hub element.
//
// Mirrors upstream cheesy-arena's game/hub.go. The scoring half of that file -- Fuel
// counting, per-shift tallies, scoring grace periods, and the shift-timing lookups
// that serve them -- is deliberately omitted: bioarena is a practice field controller
// and does not score. What remains is the rule deciding when a Hub is active, kept
// structurally identical to upstream so the two files can be diffed directly.

package game

type Hub struct {
	WonAuto bool
}

// Shift represents a distinct period during the match when Fuel is scored (and tracked separately).
type Shift int

const (
	ShiftAuto Shift = iota
	ShiftTransition
	Shift1
	Shift2
	Shift3
	Shift4
	ShiftEndgame
	ShiftPostMatch
	ShiftCount
)

// IsShiftActive returns true if the Hub is active during the given shift.
//
// Exported, unlike upstream's unexported isShiftActive, because bioarena's lighting
// drivers live outside this package. The body is otherwise unchanged.
func (hub *Hub) IsShiftActive(shift Shift) bool {
	switch shift {
	case ShiftAuto, ShiftTransition, ShiftEndgame, ShiftPostMatch:
		return true
	case Shift1, Shift3:
		return !hub.WonAuto
	case Shift2, Shift4:
		return hub.WonAuto
	default:
		return false
	}
}

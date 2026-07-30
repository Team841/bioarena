// Copyright 2026 Team 254. All Rights Reserved.
// Portions Copyright Team 841. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)
//
// Shift structure for the 2026 Hub element.
//
// Mirrors upstream cheesy-arena's game/hub.go. The scoring half of that file -- Fuel
// counting, per-shift tallies, and the scoring grace periods serving them -- is
// deliberately omitted: bioarena is a practice field controller and does not score.
// What remains is the rule deciding when a Hub is active and the shift timing the
// lighting needs, kept structurally identical to upstream so the files can be diffed.

package game

import "time"

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

// GetCurrentShiftTiming returns the current Hub shift, the amount of time remaining in it, and its duration. If the
// match is not in a valid shift, both durations are zero and the ok return value is false.
func (hub *Hub) GetCurrentShiftTiming(matchStartTime, currentTime time.Time) (Shift, time.Duration, time.Duration, bool) {
	shiftStartTime := matchStartTime
	shiftEndTime := matchStartTime.Add(GetDurationToAutoEnd())
	for _, shift := range []Shift{ShiftAuto, ShiftTransition, Shift1, Shift2, Shift3, Shift4, ShiftEndgame} {
		shiftDuration := shiftEndTime.Sub(shiftStartTime)
		if !currentTime.Before(shiftStartTime) && currentTime.Before(shiftEndTime) {
			return shift, shiftEndTime.Sub(currentTime), shiftDuration, true
		}
		shiftStartTime = shiftEndTime
		switch shift {
		case ShiftAuto:
			shiftStartTime = matchStartTime.Add(GetDurationToTeleopStart())
			shiftEndTime = shiftStartTime.Add(time.Duration(MatchTiming.TransitionShiftDurationSec) * time.Second)
		case ShiftTransition, Shift1, Shift2, Shift3:
			shiftEndTime = shiftEndTime.Add(time.Duration(MatchTiming.ShiftDurationSec) * time.Second)
		case Shift4:
			shiftEndTime = matchStartTime.Add(GetDurationToTeleopEnd())
		default:
			shiftEndTime = shiftStartTime
		}
	}
	return ShiftCount, 0, 0, false
}

// GetActiveShiftTiming returns the amount of time remaining in the current shift if the Hub is active and the duration
// of the current shift. If the Hub is not active, the remaining time is zero. If the match is not in a valid shift,
// both values are zero.
func (hub *Hub) GetActiveShiftTiming(matchStartTime, currentTime time.Time) (time.Duration, time.Duration) {
	shift, remaining, shiftDuration, ok := hub.GetCurrentShiftTiming(matchStartTime, currentTime)
	if !ok {
		return 0, 0
	}
	if hub.IsShiftActive(shift) {
		return remaining, shiftDuration
	}
	return 0, shiftDuration
}

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

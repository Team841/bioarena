// Copyright 2017 Team 254. All Rights Reserved.
// Portions Copyright Team 841. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)
//
// Game-specific period timing.

package game

import "time"

const (
	TeleopGracePeriodSec = 3
)

// MatchTiming diverges from upstream in two ways. WarmupDurationSec is a bioarena
// addition for practice starts and has no upstream counterpart, and TeleopDurationSec
// is stored flat where upstream derives it from the shift durations. The shift
// constants below carry upstream's values so the boundaries can be derived rather than
// hardcoded; TestTeleopDurationMatchesShiftBreakdown guards the two against drifting.
var MatchTiming = struct {
	WarmupDurationSec           int
	AutoDurationSec             int
	PauseDurationSec            int
	TeleopDurationSec           int
	WarningRemainingDurationSec int
	TransitionShiftDurationSec  int
	ShiftDurationSec            int
	EndgameDurationSec          int
}{0, 20, 3, 140, 30, 10, 25, 30}

// GetTeleopDurationSec derives the teleop length from the shift breakdown, as upstream
// does: one transition shift, four alliance shifts, then endgame.
func GetTeleopDurationSec() int {
	return MatchTiming.TransitionShiftDurationSec + 4*MatchTiming.ShiftDurationSec + MatchTiming.EndgameDurationSec
}

func GetDurationToAutoEnd() time.Duration {
	return time.Duration(MatchTiming.WarmupDurationSec+MatchTiming.AutoDurationSec) * time.Second
}

func GetDurationToTeleopStart() time.Duration {
	return time.Duration(
		MatchTiming.WarmupDurationSec+MatchTiming.AutoDurationSec+MatchTiming.PauseDurationSec,
	) * time.Second
}

func GetDurationToTeleopEnd() time.Duration {
	return time.Duration(
		MatchTiming.WarmupDurationSec+MatchTiming.AutoDurationSec+MatchTiming.PauseDurationSec+
			MatchTiming.TeleopDurationSec,
	) * time.Second
}

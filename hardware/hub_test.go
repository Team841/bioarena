package hardware

import (
	"testing"

	"github.com/team841/bioarena/game"
	"github.com/stretchr/testify/assert"
)

func TestTeleopSubPhaseToShift(t *testing.T) {
	for subPhase, expected := range map[TeleopSubPhase]game.Shift{
		SubPhaseTransition: game.ShiftTransition,
		SubPhaseShift1:     game.Shift1,
		SubPhaseShift2:     game.Shift2,
		SubPhaseShift3:     game.Shift3,
		SubPhaseShift4:     game.Shift4,
		SubPhaseEndGame:    game.ShiftEndgame,
		SubPhaseNone:       game.ShiftCount,
	} {
		assert.Equal(t, expected, subPhase.Shift(), "sub-phase %v", subPhase)
	}
}

// Reproduces Table 6-3 of the 2026 game manual: both HUBs active during AUTO, the
// transition shift, and endgame; during the alliance shifts the AUTO winner's HUB is
// dark for shifts 1 and 3, its opponent's for shifts 2 and 4.
func TestHubActiveReproducesManualTable(t *testing.T) {
	for _, c := range []struct {
		subPhase TeleopSubPhase
		winner   bool // HUB belongs to the alliance that won AUTO
		active   bool
	}{
		{SubPhaseTransition, true, true},
		{SubPhaseTransition, false, true},
		{SubPhaseShift1, true, false},
		{SubPhaseShift1, false, true},
		{SubPhaseShift2, true, true},
		{SubPhaseShift2, false, false},
		{SubPhaseShift3, true, false},
		{SubPhaseShift3, false, true},
		{SubPhaseShift4, true, true},
		{SubPhaseShift4, false, false},
		{SubPhaseEndGame, true, true},
		{SubPhaseEndGame, false, true},
	} {
		state := LightingState{
			Phase:          PhaseTeleop,
			TeleopSubPhase: c.subPhase,
			AutoWinner:     AllianceRed,
		}
		alliance := AllianceBlue
		if c.winner {
			alliance = AllianceRed
		}
		assert.Equal(
			t, c.active, HubActive(state, alliance),
			"sub-phase %v, wonAuto=%v", c.subPhase, c.winner,
		)
	}
}

// Exactly one HUB is dark during each alliance shift, and neither is dark outside them.
func TestHubActiveExactlyOneDarkPerAllianceShift(t *testing.T) {
	for _, subPhase := range []TeleopSubPhase{SubPhaseShift1, SubPhaseShift2, SubPhaseShift3, SubPhaseShift4} {
		state := LightingState{Phase: PhaseTeleop, TeleopSubPhase: subPhase, AutoWinner: AllianceBlue}
		assert.NotEqual(
			t, HubActive(state, AllianceRed), HubActive(state, AllianceBlue),
			"both HUBs in the same state during %v", subPhase,
		)
	}

	for _, subPhase := range []TeleopSubPhase{SubPhaseTransition, SubPhaseEndGame} {
		state := LightingState{Phase: PhaseTeleop, TeleopSubPhase: subPhase, AutoWinner: AllianceBlue}
		assert.True(t, HubActive(state, AllianceRed), "red dark during %v", subPhase)
		assert.True(t, HubActive(state, AllianceBlue), "blue dark during %v", subPhase)
	}
}

func TestHubActiveNonTeleopPhases(t *testing.T) {
	// Both HUBs are active during AUTO and after the match, per upstream's
	// isShiftActive treating ShiftAuto and ShiftPostMatch as always active.
	for _, phase := range []MatchPhase{PhaseAuto, PhaseFinished} {
		state := LightingState{Phase: phase, AutoWinner: AllianceRed}
		assert.True(t, HubActive(state, AllianceRed), "red dark during %v", phase)
		assert.True(t, HubActive(state, AllianceBlue), "blue dark during %v", phase)
	}

	// Idle and the auto/teleop pause are not scoring shifts.
	for _, phase := range []MatchPhase{PhaseIdle, PhasePause} {
		state := LightingState{Phase: phase, AutoWinner: AllianceRed}
		assert.False(t, HubActive(state, AllianceRed), "red lit during %v", phase)
		assert.False(t, HubActive(state, AllianceBlue), "blue lit during %v", phase)
	}
}

package hardware

import (
	"testing"

	"github.com/team841/bioarena/game"
	"github.com/stretchr/testify/assert"
)

// Reproduces Table 6-3 of the 2026 game manual: both HUBs active during AUTO, the
// transition shift, and endgame; during the alliance shifts the AUTO winner's HUB is
// dark for shifts 1 and 3, its opponent's for shifts 2 and 4.
func TestHubActiveReproducesManualTable(t *testing.T) {
	for _, c := range []struct {
		shift  game.Shift
		winner bool // HUB belongs to the alliance that won AUTO
		active bool
	}{
		{game.ShiftAuto, true, true},
		{game.ShiftAuto, false, true},
		{game.ShiftTransition, true, true},
		{game.ShiftTransition, false, true},
		{game.Shift1, true, false},
		{game.Shift1, false, true},
		{game.Shift2, true, true},
		{game.Shift2, false, false},
		{game.Shift3, true, false},
		{game.Shift3, false, true},
		{game.Shift4, true, true},
		{game.Shift4, false, false},
		{game.ShiftEndgame, true, true},
		{game.ShiftEndgame, false, true},
		{game.ShiftPostMatch, true, true},
		{game.ShiftPostMatch, false, true},
	} {
		state := LightingState{Phase: PhaseTeleop, Shift: c.shift, AutoWinner: AllianceRed}
		alliance := AllianceBlue
		if c.winner {
			alliance = AllianceRed
		}
		assert.Equal(
			t, c.active, HubActive(state, alliance),
			"shift %v, wonAuto=%v", c.shift, c.winner,
		)
	}
}

// Exactly one HUB is dark during each alliance shift, and neither is dark outside them.
func TestHubActiveExactlyOneDarkPerAllianceShift(t *testing.T) {
	for _, shift := range []game.Shift{game.Shift1, game.Shift2, game.Shift3, game.Shift4} {
		state := LightingState{Phase: PhaseTeleop, Shift: shift, AutoWinner: AllianceBlue}
		assert.NotEqual(
			t, HubActive(state, AllianceRed), HubActive(state, AllianceBlue),
			"both HUBs in the same state during %v", shift,
		)
	}

	alwaysActive := []game.Shift{game.ShiftAuto, game.ShiftTransition, game.ShiftEndgame, game.ShiftPostMatch}
	for _, shift := range alwaysActive {
		state := LightingState{Phase: PhaseTeleop, Shift: shift, AutoWinner: AllianceBlue}
		assert.True(t, HubActive(state, AllianceRed), "red dark during %v", shift)
		assert.True(t, HubActive(state, AllianceBlue), "blue dark during %v", shift)
	}
}

// ShiftCount marks "not in a shift" -- idle and the auto/teleop pause. Upstream's
// isShiftActive returns false for it, so the HUB is dark.
func TestHubActiveDarkWhenNotInShift(t *testing.T) {
	state := LightingState{Phase: PhaseIdle, Shift: game.ShiftCount, AutoWinner: AllianceRed}
	assert.False(t, HubActive(state, AllianceRed))
	assert.False(t, HubActive(state, AllianceBlue))
}

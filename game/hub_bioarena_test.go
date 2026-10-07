package game

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The ordering must match upstream cheesy-arena's const block exactly. ShiftCount is
// both the sentinel for "not in a shift" and the array length upstream sizes its
// per-shift tallies with, so a reordering here would silently diverge from it.
func TestShiftConstantsMatchUpstream(t *testing.T) {
	assert.Equal(t, Shift(0), ShiftAuto)
	assert.Equal(t, Shift(1), ShiftTransition)
	assert.Equal(t, Shift(2), Shift1)
	assert.Equal(t, Shift(3), Shift2)
	assert.Equal(t, Shift(4), Shift3)
	assert.Equal(t, Shift(5), Shift4)
	assert.Equal(t, Shift(6), ShiftEndgame)
	assert.Equal(t, Shift(7), ShiftPostMatch)
	assert.Equal(t, Shift(8), ShiftCount)
}

// Mirrors upstream's isShiftActive: always-active shifts, then the alternation keyed
// on WonAuto.
func TestIsShiftActive(t *testing.T) {
	winner := &Hub{WonAuto: true}
	loser := &Hub{WonAuto: false}

	for _, shift := range []Shift{ShiftAuto, ShiftTransition, ShiftEndgame, ShiftPostMatch} {
		assert.True(t, winner.IsShiftActive(shift), "winner inactive during %v", shift)
		assert.True(t, loser.IsShiftActive(shift), "loser inactive during %v", shift)
	}

	// The AUTO winner's Hub is dark for shifts 1 and 3, its opponent's for 2 and 4.
	for _, shift := range []Shift{Shift1, Shift3} {
		assert.False(t, winner.IsShiftActive(shift), "winner active during %v", shift)
		assert.True(t, loser.IsShiftActive(shift), "loser inactive during %v", shift)
	}
	for _, shift := range []Shift{Shift2, Shift4} {
		assert.True(t, winner.IsShiftActive(shift), "winner inactive during %v", shift)
		assert.False(t, loser.IsShiftActive(shift), "loser active during %v", shift)
	}

	// ShiftCount is not a shift.
	assert.False(t, winner.IsShiftActive(ShiftCount))
	assert.False(t, loser.IsShiftActive(ShiftCount))
}

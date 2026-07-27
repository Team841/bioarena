package game

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TeleopDurationSec is stored flat, where upstream derives it from the shift
// breakdown. Guard the two against drifting apart: a change to either the flat value
// or a shift duration that does not keep them consistent moves the shift boundaries
// out from under the HUB lighting.
func TestTeleopDurationMatchesShiftBreakdown(t *testing.T) {
	assert.Equal(t, MatchTiming.TeleopDurationSec, GetTeleopDurationSec())
}

// Values verified against the 2026 game manual and upstream cheesy-arena.
func TestMatchTimingMatchesUpstream(t *testing.T) {
	assert.Equal(t, 20, MatchTiming.AutoDurationSec)
	assert.Equal(t, 3, MatchTiming.PauseDurationSec)
	assert.Equal(t, 10, MatchTiming.TransitionShiftDurationSec)
	assert.Equal(t, 25, MatchTiming.ShiftDurationSec)
	assert.Equal(t, 30, MatchTiming.EndgameDurationSec)
	assert.Equal(t, 140, GetTeleopDurationSec())
}

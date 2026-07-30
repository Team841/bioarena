package field

import (
	"testing"
	"time"

	"github.com/team841/bioarena/game"
	"github.com/team841/bioarena/hardware"
	"github.com/team841/bioarena/led"
	"github.com/stretchr/testify/assert"
)

// teleopOffset returns a match start time placing the given remaining teleop seconds.
func teleopOffset(remaining int) time.Time {
	elapsed := game.GetDurationToTeleopStart() +
		time.Duration(game.MatchTiming.TeleopDurationSec-remaining)*time.Second
	return time.Now().Add(-elapsed)
}

// The dark HUB is the one whose alliance is inactive for the current shift, matching
// Table 6-3. Red winning AUTO means red is dark for shifts 1 and 3.
func TestUpdateHubLedsAlternatesWithAutoWinner(t *testing.T) {
	for _, c := range []struct {
		remaining int
		shift     string
		redMode   led.Mode
		blueMode  led.Mode
	}{
		{120, "Shift1", led.OffMode, led.BlueMode},
		{95, "Shift2", led.RedMode, led.OffMode},
		{70, "Shift3", led.OffMode, led.BlueMode},
		{45, "Shift4", led.RedMode, led.OffMode},
		{20, "Endgame", led.RedMode, led.BlueMode},
	} {
		arena := setupTestArena(t)
		arena.AutoWinner = hardware.AllianceRed
		arena.MatchState = TeleopPeriod
		arena.MatchStartTime = teleopOffset(c.remaining)

		arena.updateTeleopHubLeds(time.Now())

		redMode, blueMode := arena.Leds.GetModes()
		assert.Equal(t, c.redMode, redMode, "%s red", c.shift)
		assert.Equal(t, c.blueMode, blueMode, "%s blue", c.shift)
	}
}

// Reversing the AUTO winner reverses which HUB is dark.
func TestUpdateHubLedsReversesForBlueAutoWinner(t *testing.T) {
	arena := setupTestArena(t)
	arena.AutoWinner = hardware.AllianceBlue
	arena.MatchState = TeleopPeriod
	arena.MatchStartTime = teleopOffset(120) // Shift1

	arena.updateTeleopHubLeds(time.Now())

	redMode, blueMode := arena.Leds.GetModes()
	assert.Equal(t, led.RedMode, redMode)
	assert.Equal(t, led.OffMode, blueMode)
}

// The HUB about to go dark pulses during the 3s window before the boundary.
func TestUpdateHubLedsPulsesBeforeDeactivation(t *testing.T) {
	arena := setupTestArena(t)
	arena.AutoWinner = hardware.AllianceRed
	arena.MatchState = TeleopPeriod

	// Two seconds before the Shift1 boundary: blue is active now and goes dark next,
	// so blue pulses.
	arena.MatchStartTime = teleopOffset(107)
	arena.updateTeleopHubLeds(time.Now())
	_, blueMode := arena.Leds.GetModes()
	assert.Equal(t, led.BluePulseMode, blueMode)
}

// The transition shift runs the advantage sweep on the AUTO winner's HUB.
func TestUpdateHubLedsTransitionAdvantage(t *testing.T) {
	arena := setupTestArena(t)
	arena.AutoWinner = hardware.AllianceRed
	arena.MatchState = TeleopPeriod
	arena.MatchStartTime = teleopOffset(137) // transition shift, outside the warning window

	arena.updateTeleopHubLeds(time.Now())

	redMode, blueMode := arena.Leds.GetModes()
	assert.Equal(t, led.RedAdvantageMode, redMode)
	assert.Equal(t, led.BlueMode, blueMode)
}

// AUTO runs the startup fill on both HUBs.
func TestUpdateHubLedsAutoStartup(t *testing.T) {
	arena := setupTestArena(t)
	arena.MatchState = AutoPeriod
	arena.MatchStartTime = time.Now()

	arena.updateHubLeds(time.Now())

	redMode, blueMode := arena.Leds.GetModes()
	assert.Equal(t, led.RedStartupMode, redMode)
	assert.Equal(t, led.BlueStartupMode, blueMode)
}

// With no sACN address configured the controller runs its sequences but sends nothing,
// so the match loop must not error.
func TestUpdateHubLedsNoAddressDoesNotError(t *testing.T) {
	arena := setupTestArena(t)
	arena.MatchState = TeleopPeriod
	arena.MatchStartTime = teleopOffset(100)

	assert.NotPanics(t, func() { arena.updateHubLeds(time.Now()) })
}

package field

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/team841/bioarena/game"
	"github.com/team841/bioarena/hardware"
	"github.com/team841/bioarena/led"
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

// The settings pick the output: sACN or Art-Net to upstream's fixtures, or the relays. On
// this platform the relay GPIO cannot open, which must leave a working sequence behind rather
// than a nil output that panics the arena loop.
func TestApplyHubLedSettingsPicksTheOutput(t *testing.T) {
	arena := setupTestArena(t)

	_, isSacn := arena.Leds.(*led.Controller)
	assert.True(t, isSacn, "sACN is the default")

	arena.EventSettings.HubLedsArtNet = true
	arena.applyHubLedSettings(arena.EventSettings)
	_, isArtNet := arena.Leds.(*led.ArtNetController)
	assert.True(t, isArtNet, "ticking the box should switch to Art-Net")

	arena.EventSettings.HubLedsArtNet = false
	arena.applyHubLedSettings(arena.EventSettings)
	_, isSacn = arena.Leds.(*led.Controller)
	assert.True(t, isSacn, "unticking it should switch back")

	arena.EventSettings.HubRelays = true
	arena.applyHubLedSettings(arena.EventSettings)
	assert.NotNil(t, arena.Leds)
	arena.MatchState = AutoPeriod
	assert.NotPanics(t, func() { arena.updateHubLeds(time.Now()) })
}

// The outgoing output is released before its replacement is built: the relay lines have to
// go back to the kernel before the new relays can open the same pins.
func TestApplyHubLedSettingsReleasesThePreviousOutput(t *testing.T) {
	arena := setupTestArena(t)
	previous := &closingLeds{}
	arena.Leds = previous

	arena.applyHubLedSettings(arena.EventSettings)
	assert.True(t, previous.closed)
	assert.NotSame(t, previous, arena.Leds)
}

// closingLeds is a ledController that records being released, as the relays are.
type closingLeds struct {
	failingLeds
	closed bool
}

func (c *closingLeds) Close() { c.closed = true }

// failingLeds is a ledController whose Update fails on demand.
type failingLeds struct {
	fail bool
}

func (f *failingLeds) SetMode(_, _ led.Mode)          {}
func (f *failingLeds) GetModes() (led.Mode, led.Mode) { return led.OffMode, led.OffMode }

func (f *failingLeds) Update() error {
	if f.fail {
		return fmt.Errorf("no route to host")
	}
	return nil
}

// updateHubLeds runs on the arena loop at 100 Hz. Logging every failure buries the journal
// under a hundred identical lines a second whenever a receiver is unplugged or the address
// points at hardware that is not there.
func TestHubLedFailureIsLoggedOncePerSpell(t *testing.T) {
	arena := setupTestArena(t)
	leds := &failingLeds{fail: true}
	arena.Leds = leds

	var logged bytes.Buffer
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	for i := 0; i < 50; i++ {
		arena.updateHubLeds(time.Now())
	}
	assert.Equal(t, 1, strings.Count(logged.String(), "Failed to update hub LEDs"),
		"a sustained failure should be reported once, not once per tick")

	// Recovery is worth a line, and re-arms the report for the next spell.
	logged.Reset()
	leds.fail = false
	arena.updateHubLeds(time.Now())
	assert.Contains(t, logged.String(), "updating again")

	logged.Reset()
	leds.fail = true
	for i := 0; i < 10; i++ {
		arena.updateHubLeds(time.Now())
	}
	assert.Equal(t, 1, strings.Count(logged.String(), "Failed to update hub LEDs"),
		"a new failure spell should report again")
}

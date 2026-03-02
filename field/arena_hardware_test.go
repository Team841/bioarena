package field

import (
	"testing"

	"github.com/Team254/cheesy-arena/hardware"
	"github.com/stretchr/testify/assert"
)

// --- teleopSubPhase ---

func TestTeleopSubPhaseBoundaries(t *testing.T) {
	cases := []struct {
		remaining int
		want      hardware.TeleopSubPhase
	}{
		{135, hardware.SubPhaseTransition}, // above transition window
		{131, hardware.SubPhaseTransition},
		{130, hardware.SubPhaseShift1}, // boundary: <=130 → Shift1
		{106, hardware.SubPhaseShift1},
		{105, hardware.SubPhaseShift2}, // boundary: <=105 → Shift2
		{81, hardware.SubPhaseShift2},
		{80, hardware.SubPhaseShift3}, // boundary: <=80 → Shift3
		{56, hardware.SubPhaseShift3},
		{55, hardware.SubPhaseShift4}, // boundary: <=55 → Shift4
		{31, hardware.SubPhaseShift4},
		{30, hardware.SubPhaseEndGame}, // boundary: <=30 → EndGame
		{0, hardware.SubPhaseEndGame},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, teleopSubPhase(c.remaining), "remaining=%d", c.remaining)
	}
}

// --- shiftWarning ---

func TestShiftWarningWindows(t *testing.T) {
	// Warning should fire for the 3s window BEFORE each shift boundary.
	// Boundary at 130: warning at [130,133)
	assert.True(t, shiftWarning(130), "130")
	assert.True(t, shiftWarning(132), "132")
	assert.False(t, shiftWarning(133), "133 — outside window")
	assert.False(t, shiftWarning(129), "129 — already past boundary")

	// Boundary at 105: warning at [105,108)
	assert.True(t, shiftWarning(105))
	assert.True(t, shiftWarning(107))
	assert.False(t, shiftWarning(108))

	// Boundary at 80: warning at [80,83)
	assert.True(t, shiftWarning(80))
	assert.True(t, shiftWarning(82))
	assert.False(t, shiftWarning(83))

	// Boundary at 55: warning at [55,58)
	assert.True(t, shiftWarning(55))
	assert.True(t, shiftWarning(57))
	assert.False(t, shiftWarning(58))

	// No warning for EndGame
	assert.False(t, shiftWarning(29))
	assert.False(t, shiftWarning(0))
}

// --- computeLightingState ---

func TestComputeLightingStatePhaseMapping(t *testing.T) {
	arena := setupTestArena(t)
	arena.AutoWinner = hardware.AllianceRed

	arena.MatchState = PreMatch
	ls := arena.computeLightingState(0)
	assert.Equal(t, hardware.PhaseIdle, ls.Phase)

	arena.MatchState = AutoPeriod
	ls = arena.computeLightingState(3)
	assert.Equal(t, hardware.PhaseAuto, ls.Phase)
	assert.Equal(t, hardware.AllianceRed, ls.AutoWinner)

	arena.MatchState = PausePeriod
	ls = arena.computeLightingState(18)
	assert.Equal(t, hardware.PhasePause, ls.Phase)

	arena.MatchState = PostMatch
	ls = arena.computeLightingState(160)
	assert.Equal(t, hardware.PhaseFinished, ls.Phase)
}

// --- EStopPanel polling integration ---

// recordingPanel records every event delivered to handleTeamStop.
// It implements hardware.EStopPanel and returns a fixed event list.
type recordingPanel struct {
	events []hardware.EStopEvent
}

func (r *recordingPanel) Poll() []hardware.EStopEvent { return r.events }

func TestEStopPanelPollSingleStation(t *testing.T) {
	arena := setupTestArena(t)
	panel := &recordingPanel{
		events: []hardware.EStopEvent{{Station: "R1", IsAStop: false}},
	}
	arena.EStopPanels = []hardware.EStopPanel{panel}

	// Trigger polling manually the same way Update() does.
	for _, p := range arena.EStopPanels {
		for _, ev := range p.Poll() {
			arena.handleTeamStop(ev.Station, !ev.IsAStop, ev.IsAStop)
		}
	}

	assert.True(t, arena.AllianceStations["R1"].EStop.Load())
	assert.False(t, arena.AllianceStations["B1"].EStop.Load())
}

func TestEStopPanelPollAllStations(t *testing.T) {
	arena := setupTestArena(t)
	panel := &recordingPanel{
		events: []hardware.EStopEvent{{Station: "all", IsAStop: false}},
	}
	arena.EStopPanels = []hardware.EStopPanel{panel}

	for _, p := range arena.EStopPanels {
		for _, ev := range p.Poll() {
			if ev.Station == "all" {
				for _, s := range []string{"R1", "R2", "R3", "B1", "B2", "B3"} {
					arena.handleTeamStop(s, !ev.IsAStop, ev.IsAStop)
				}
			} else {
				arena.handleTeamStop(ev.Station, !ev.IsAStop, ev.IsAStop)
			}
		}
	}

	for _, station := range []string{"R1", "R2", "R3", "B1", "B2", "B3"} {
		assert.True(t, arena.AllianceStations[station].EStop.Load(), "station=%s", station)
	}
}

// --- NoopFieldLights integration ---

func TestNoopFieldLightsIntegration(t *testing.T) {
	arena := setupTestArena(t)
	// Default is already Noop; confirm SetState never errors.
	states := []hardware.LightingState{
		{Phase: hardware.PhaseIdle},
		{Phase: hardware.PhaseAuto},
		{Phase: hardware.PhasePause},
		{Phase: hardware.PhaseTeleop, TeleopSubPhase: hardware.SubPhaseShift1, AutoWinner: hardware.AllianceBlue},
		{Phase: hardware.PhaseFinished},
	}
	for _, s := range states {
		assert.NoError(t, arena.FieldLights.SetState(s))
	}
}

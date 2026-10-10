package field

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/team841/bioarena/game"
	"github.com/team841/bioarena/hardware"
)

// --- teleopShift ---

func TestTeleopShiftBoundaries(t *testing.T) {
	cases := []struct {
		remaining int
		want      game.Shift
	}{
		{135, game.ShiftTransition}, // above transition window
		{131, game.ShiftTransition},
		{130, game.Shift1}, // boundary: <=130 → Shift1
		{106, game.Shift1},
		{105, game.Shift2}, // boundary: <=105 → Shift2
		{81, game.Shift2},
		{80, game.Shift3}, // boundary: <=80 → Shift3
		{56, game.Shift3},
		{55, game.Shift4}, // boundary: <=55 → Shift4
		{31, game.Shift4},
		{30, game.ShiftEndgame}, // boundary: <=30 → EndGame
		{0, game.ShiftEndgame},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, teleopShift(c.remaining), "remaining=%d", c.remaining)
	}
}

// --- shiftChangeSounds ---

// The cue marks the four HUB handovers and nothing else. The shifts that do not sound are
// the point of the test: each already has a cue of its own, and stacking two on one moment
// is what this excludes.
func TestShiftChangeSoundsOnlyOnHandovers(t *testing.T) {
	for _, shift := range []game.Shift{game.Shift1, game.Shift2, game.Shift3, game.Shift4} {
		assert.True(t, shiftChangeSounds(shift), "shift %d should sound", shift)
	}
	for _, shift := range []game.Shift{
		game.ShiftAuto,       // "start"
		game.ShiftTransition, // "resume"
		game.ShiftEndgame,    // "warning"
		game.ShiftPostMatch,  // "end"
		game.ShiftCount,      // not a shift at all
	} {
		assert.False(t, shiftChangeSounds(shift), "shift %d should be silent", shift)
	}
}

// The cue fires on the tick currentShift changes, so the boundaries it fires on are the ones
// teleopShift produces. Walking the teleop second by second proves there are exactly four,
// and that they sit on the shift boundaries rather than near them.
func TestShiftChangeSoundsMatchLightingBoundaries(t *testing.T) {
	var boundaries []int
	previous := teleopShift(200) // before the first boundary
	for remaining := 200; remaining >= 0; remaining-- {
		current := teleopShift(remaining)
		if current != previous && shiftChangeSounds(current) {
			boundaries = append(boundaries, remaining)
		}
		previous = current
	}
	assert.Equal(t, []int{130, 105, 80, 55}, boundaries)
}

// --- currentShift ---

// The shift spans the whole match, as upstream models it: AUTO and post-match have their own
// shifts, and states that are not a shift report ShiftCount.
func TestCurrentShift(t *testing.T) {
	arena := setupTestArena(t)

	arena.MatchState = PreMatch
	assert.Equal(t, game.ShiftCount, arena.currentShift(0))

	arena.MatchState = AutoPeriod
	assert.Equal(t, game.ShiftAuto, arena.currentShift(3))

	arena.MatchState = PausePeriod
	assert.Equal(t, game.ShiftCount, arena.currentShift(21))

	arena.MatchState = TeleopPeriod
	teleopStart := game.GetDurationToTeleopStart().Seconds()
	assert.Equal(t, game.ShiftTransition, arena.currentShift(teleopStart+5))
	assert.Equal(t, game.Shift1, arena.currentShift(teleopStart+15))

	arena.MatchState = PostMatch
	assert.Equal(t, game.ShiftPostMatch, arena.currentShift(165))
}

// --- EStopPanel polling integration ---

// recordingPanel implements hardware.EStopPanel and returns a fixed snapshot.
type recordingPanel struct {
	inputs []hardware.InputState
}

func (r *recordingPanel) Poll() []hardware.InputState { return r.inputs }

// pollPanels drives the panels exactly the way Update() does.
func pollPanels(arena *Arena) {
	for _, p := range arena.EStopPanels {
		for _, input := range p.Poll() {
			stations := []string{input.Station}
			if input.Station == "all" {
				stations = allStationNames
			}
			for _, station := range stations {
				arena.handlePanelInput(station, input)
			}
		}
	}
}

// A panel's field e-stop stops all six stations, and on a normally-closed input an empty
// pin reads as pressed. So it counts only once that panel is ticked as having the button:
// until then a panel built without one must not hold the field.
func TestPanelFieldEStopHonouredOnlyWhenTicked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(hardware.PanelSnapshot{
			Alliance: "blue",
			AgeMs:    5,
			Inputs: []hardware.InputState{
				{Station: "B1", State: hardware.StopOK},
				{Station: "all", State: hardware.StopActive},
			},
		})
	}))
	defer srv.Close()

	arena := setupTestArena(t)
	arena.EventSettings.BlueEStopPanelAddress = srv.URL
	assert.Nil(t, arena.Database.UpdateEventSettings(arena.EventSettings))
	assert.Nil(t, arena.LoadSettings())

	pollPanels(arena)
	for _, station := range allStationNames {
		assert.False(t, arena.AllianceStations[station].EStop.Load(), "unticked: %s must not stop", station)
	}

	arena.EventSettings.BlueEStopPanelFieldEStop = true
	assert.Nil(t, arena.Database.UpdateEventSettings(arena.EventSettings))
	assert.Nil(t, arena.LoadSettings())

	pollPanels(arena)
	for _, station := range allStationNames {
		assert.True(t, arena.AllianceStations[station].EStop.Load(), "ticked: %s must stop", station)
	}
}

func TestEStopPanelPollSingleStation(t *testing.T) {
	arena := setupTestArena(t)
	arena.EStopPanels = []hardware.EStopPanel{&recordingPanel{
		inputs: []hardware.InputState{{Station: "R1", State: hardware.StopActive}},
	}}

	pollPanels(arena)

	assert.True(t, arena.AllianceStations["R1"].EStop.Load())
	assert.False(t, arena.AllianceStations["B1"].EStop.Load())
}

func TestEStopPanelPollAllStations(t *testing.T) {
	arena := setupTestArena(t)
	arena.EStopPanels = []hardware.EStopPanel{&recordingPanel{
		inputs: []hardware.InputState{{Station: "all", State: hardware.StopActive}},
	}}

	pollPanels(arena)

	for _, station := range allStationNames {
		assert.True(t, arena.AllianceStations[station].EStop.Load(), "station=%s", station)
	}
}

func TestEStopPanelReleasedInputClearsStop(t *testing.T) {
	arena := setupTestArena(t)
	panel := &recordingPanel{
		inputs: []hardware.InputState{{Station: "R1", State: hardware.StopActive}},
	}
	arena.EStopPanels = []hardware.EStopPanel{panel}
	pollPanels(arena)
	assert.True(t, arena.AllianceStations["R1"].EStop.Load())

	// An explicit released reading is what clears the stop outside a match;
	// with the old presence-based protocol there was nothing to report here.
	panel.inputs = []hardware.InputState{{Station: "R1", State: hardware.StopOK}}
	pollPanels(arena)
	assert.False(t, arena.AllianceStations["R1"].EStop.Load())
}

func TestEStopPanelFaultStopsStation(t *testing.T) {
	arena := setupTestArena(t)
	arena.EStopPanels = []hardware.EStopPanel{&recordingPanel{
		inputs: []hardware.InputState{
			{Station: "R2", State: hardware.StopFault, Fault: hardware.FaultBothOpen},
		},
	}}

	pollPanels(arena)

	assert.True(t, arena.AllianceStations["R2"].EStop.Load(), "a wiring fault must stop the station")
	assert.Equal(t, uint32(hardware.FaultBothOpen), arena.AllianceStations["R2"].EStopFault.Load())
}

func TestEStopPanelFaultBlocksMatchStart(t *testing.T) {
	arena := setupTestArena(t)
	// R1 is checked first, so its fault is what the operator is told about.
	arena.AllianceStations["R1"].EStopFault.Store(uint32(hardware.FaultBothClosed))

	err := arena.checkCanStartMatch()
	assert.ErrorContains(t, err, "wiring fault")
	assert.ErrorContains(t, err, "R1")
}

func TestEStopPanelFaultClearsWhenWiringRepaired(t *testing.T) {
	arena := setupTestArena(t)
	panel := &recordingPanel{
		inputs: []hardware.InputState{
			{Station: "R1", State: hardware.StopFault, Fault: hardware.FaultBothOpen},
		},
	}
	arena.EStopPanels = []hardware.EStopPanel{panel}
	pollPanels(arena)
	assert.Equal(t, uint32(hardware.FaultBothOpen), arena.AllianceStations["R1"].EStopFault.Load())

	// The fault tracks the live wiring rather than latching, so a repair that
	// the panel reports as healthy takes the fault with it.
	panel.inputs = []hardware.InputState{{Station: "R1", State: hardware.StopOK}}
	pollPanels(arena)
	assert.Equal(t, uint32(hardware.FaultNone), arena.AllianceStations["R1"].EStopFault.Load())
	assert.False(t, arena.AllianceStations["R1"].EStop.Load())
	if err := arena.checkCanStartMatch(); err != nil {
		assert.NotContains(t, err.Error(), "wiring fault")
	}
}

func TestAStopInputDoesNotClearLatchedEStop(t *testing.T) {
	arena := setupTestArena(t)
	arena.EStopPanels = []hardware.EStopPanel{&recordingPanel{
		inputs: []hardware.InputState{
			{Station: "R1", IsAStop: false, State: hardware.StopActive},
			{Station: "R1", IsAStop: true, State: hardware.StopActive},
		},
	}}

	pollPanels(arena)

	// Each input only speaks for its own half of the station's state.
	assert.True(t, arena.AllianceStations["R1"].EStop.Load(), "an a-stop must not clear an e-stop")
	assert.True(t, arena.AllianceStations["R1"].AStop.Load())
}

// --- GPIO FieldEStop arena integration ---

// mockFieldEStop simulates a dual-channel GPIO field e-stop, latching the same
// way the real driver does.
type mockFieldEStop struct {
	live         hardware.StopState // what the pins currently read
	liveFault    hardware.FaultKind
	latched      hardware.StopState
	latchedFault hardware.FaultKind
	closed       bool
}

func (m *mockFieldEStop) State() (hardware.StopState, hardware.FaultKind) {
	if m.live == hardware.StopFault || (m.live == hardware.StopActive && m.latched == hardware.StopOK) {
		m.latched, m.latchedFault = m.live, m.liveFault
	}
	return m.latched, m.latchedFault
}

func (m *mockFieldEStop) Close() { m.closed = true }

func (m *mockFieldEStop) Clear() {
	if m.live == hardware.StopOK {
		m.latched, m.latchedFault = hardware.StopOK, hardware.FaultNone
	}
}

// pollFieldEStop drives the field e-stop the way Update() does.
func pollFieldEStop(arena *Arena) {
	state, fault := arena.FieldEStop.State()
	arena.fieldEStopFault.Store(uint32(fault))
	if state != hardware.StopOK && !arena.fieldEStopActive.Load() {
		arena.fieldEStopActive.Store(true)
		for _, as := range arena.AllianceStations {
			as.EStop.Store(true)
		}
	}
}

func TestFieldEStopDisablesAllStations(t *testing.T) {
	arena := setupTestArena(t)
	mock := &mockFieldEStop{}
	arena.FieldEStop = mock

	mock.live = hardware.StopActive
	pollFieldEStop(arena)

	assert.True(t, arena.fieldEStopActive.Load())
	for _, station := range allStationNames {
		assert.True(t, arena.AllianceStations[station].EStop.Load(), "station=%s should be e-stopped", station)
	}
}

func TestFieldEStopFaultDisablesAllStations(t *testing.T) {
	arena := setupTestArena(t)
	mock := &mockFieldEStop{live: hardware.StopFault, liveFault: hardware.FaultBothOpen}
	arena.FieldEStop = mock

	pollFieldEStop(arena)

	assert.True(t, arena.fieldEStopActive.Load())
	assert.Equal(t, uint32(hardware.FaultBothOpen), arena.fieldEStopFault.Load())
	for _, station := range allStationNames {
		assert.True(t, arena.AllianceStations[station].EStop.Load(), "station=%s should be e-stopped", station)
	}
}

func TestFieldEStopLatchPersistsAfterRelease(t *testing.T) {
	arena := setupTestArena(t)
	mock := &mockFieldEStop{live: hardware.StopActive}
	arena.FieldEStop = mock

	pollFieldEStop(arena)
	mock.live = hardware.StopOK

	state, _ := arena.FieldEStop.State()
	assert.Equal(t, hardware.StopActive, state, "latch must persist after button release")
	assert.True(t, arena.fieldEStopActive.Load())
}

func TestFieldEStopClearReleasedButton(t *testing.T) {
	arena := setupTestArena(t)
	mock := &mockFieldEStop{live: hardware.StopActive}
	arena.FieldEStop = mock
	pollFieldEStop(arena)

	// Release button and clear.
	mock.live = hardware.StopOK
	arena.ClearFieldEStop()

	assert.False(t, arena.fieldEStopActive.Load())
	for _, station := range allStationNames {
		assert.False(t, arena.AllianceStations[station].EStop.Load(), "station=%s should be cleared", station)
	}
}

func TestFieldEStopClearNoopWhileHeld(t *testing.T) {
	arena := setupTestArena(t)
	mock := &mockFieldEStop{live: hardware.StopActive}
	arena.FieldEStop = mock
	pollFieldEStop(arena)

	// Try to clear while still held — should be no-op.
	arena.ClearFieldEStop()
	assert.True(t, arena.fieldEStopActive.Load(), "clear while held must be no-op")
}

func TestFieldEStopClearNoopWhileFaulted(t *testing.T) {
	arena := setupTestArena(t)
	mock := &mockFieldEStop{live: hardware.StopFault, liveFault: hardware.FaultBothOpen}
	arena.FieldEStop = mock
	pollFieldEStop(arena)

	// An operator must not be able to acknowledge away a fault that is still live.
	arena.ClearFieldEStop()
	assert.True(t, arena.fieldEStopActive.Load(), "clear while faulted must be no-op")
	assert.Equal(t, uint32(hardware.FaultBothOpen), arena.fieldEStopFault.Load())
}

func TestFieldEStopBlocksMatchStart(t *testing.T) {
	arena := setupTestArena(t)
	arena.fieldEStopActive.Store(true)

	err := arena.checkCanStartMatch()
	assert.ErrorContains(t, err, "field emergency stop")
}

func TestFieldEStopFaultBlocksMatchStart(t *testing.T) {
	arena := setupTestArena(t)
	arena.fieldEStopFault.Store(uint32(hardware.FaultUnreachable))

	err := arena.checkCanStartMatch()
	assert.ErrorContains(t, err, "wiring fault")
}

// --- field e-stop lifecycle ---

// LoadSettings runs on every settings save, and the kernel gives a GPIO line to one
// requester at a time. Without releasing the previous panel the second save of a run cannot
// open the pins, falls back to NoopFieldEStopPanel, and reports StopOK forever -- a field
// showing a healthy e-stop with nothing watching the button.
func TestLoadSettingsReleasesThePreviousFieldEStop(t *testing.T) {
	arena := setupTestArena(t)

	previous := &mockFieldEStop{}
	arena.FieldEStop = previous
	assert.Nil(t, arena.LoadSettings())

	assert.True(t, previous.closed, "the previous panel's GPIO lines were never released")
	assert.NotSame(t, previous, arena.FieldEStop, "the panel should have been rebuilt")
}

// The badge cannot tell a healthy field e-stop from an absent one without this: a noop
// panel reports StopOK forever, exactly like a working button at rest.
func TestFieldEStopMonitoredReflectsRealHardware(t *testing.T) {
	arena := setupTestArena(t)

	// No pin configured, so the noop panel is installed and nothing is watching.
	assert.Nil(t, arena.LoadSettings())
	assert.False(t, arena.fieldEStopMonitored.Load())

	status := arena.generateArenaStatusMessage()
	monitored := reflect.ValueOf(status).Elem().FieldByName("FieldEStopMonitored")
	assert.True(t, monitored.IsValid(), "arena status should carry FieldEStopMonitored")
	assert.False(t, monitored.Bool())
}

// --- e-stop panel badges ---

// statusPanel is an EStopPanel that also reports a status, standing in for a network panel.
type statusPanel struct {
	alliance string
	status   string
}

func (p *statusPanel) Poll() []hardware.InputState { return nil }
func (p *statusPanel) Alliance() string            { return p.alliance }
func (p *statusPanel) Status() string              { return p.status }

// The badges answer "is the panel on?" per alliance, in the same vocabulary as the access
// point and switch so they share colours. An alliance with no panel reads DISABLED rather
// than red: not configured is not the same as broken.
func TestArenaStatusCarriesPanelStatusPerAlliance(t *testing.T) {
	arena := setupTestArena(t)
	arena.EStopPanels = []hardware.EStopPanel{
		&statusPanel{alliance: "red", status: hardware.PanelStatusActive},
	}

	status := reflect.ValueOf(arena.generateArenaStatusMessage()).Elem()
	assert.Equal(t, hardware.PanelStatusActive, status.FieldByName("RedPanelStatus").String())
	assert.Equal(t, hardware.PanelStatusDisabled, status.FieldByName("BluePanelStatus").String(),
		"no blue panel configured")

	// A panel that does not report a status -- the plain interface -- has no badge to show,
	// and must not be mistaken for the other alliance's.
	arena.EStopPanels = append(arena.EStopPanels, &recordingPanel{})
	status = reflect.ValueOf(arena.generateArenaStatusMessage()).Elem()
	assert.Equal(t, hardware.PanelStatusDisabled, status.FieldByName("BluePanelStatus").String())
}

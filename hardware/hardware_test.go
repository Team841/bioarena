package hardware

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/team841/bioarena/game"
)

// mockLineReader simulates a GPIO pin value for unit testing.
type mockLineReader struct {
	val int
	err error
}

func (m *mockLineReader) Value() (int, error) { return m.val, m.err }

// newTestGpioPanel builds a dual-channel panel with a zero discrepancy window,
// so a mismatch is classified as a fault on the first reading. The window
// itself is covered by the DiscrepancyFilter tests, which can drive time
// directly.
func newTestGpioPanel(nc, no int) (*GpioFieldEStopPanel, *mockLineReader, *mockLineReader) {
	ncLine := &mockLineReader{val: nc}
	noLine := &mockLineReader{val: no}
	return &GpioFieldEStopPanel{nc: ncLine, no: noLine}, ncLine, noLine
}

// newTestSingleChannelPanel builds a NO-only panel, as a legacy config produces.
func newTestSingleChannelPanel(no int) (*GpioFieldEStopPanel, *mockLineReader) {
	noLine := &mockLineReader{val: no}
	return &GpioFieldEStopPanel{no: noLine}, noLine
}

func TestNoopFieldLightsImplementsInterface(t *testing.T) {
	var fl FieldLights = &NoopFieldLights{}
	assert.NoError(t, fl.SetState(LightingState{Phase: PhaseAuto}))
	assert.NoError(t, fl.SetState(LightingState{Phase: PhaseTeleop}))
}

func TestNoopEStopPanelImplementsInterface(t *testing.T) {
	var ep EStopPanel = &NoopEStopPanel{}
	assert.Nil(t, ep.Poll())
}

func TestLightingStateEquality(t *testing.T) {
	s1 := LightingState{Phase: PhaseTeleop, Shift: game.Shift1, AutoWinner: AllianceRed, ShiftWarning: false}
	s2 := LightingState{Phase: PhaseTeleop, Shift: game.Shift1, AutoWinner: AllianceRed, ShiftWarning: false}
	s3 := LightingState{Phase: PhaseTeleop, Shift: game.Shift1, AutoWinner: AllianceRed, ShiftWarning: true}

	assert.Equal(t, s1, s2)
	assert.NotEqual(t, s1, s3)
}

func TestMatchPhaseConstants(t *testing.T) {
	assert.Equal(t, MatchPhase(0), PhaseIdle)
	assert.Equal(t, MatchPhase(1), PhaseAuto)
	assert.Equal(t, MatchPhase(2), PhasePause)
	assert.Equal(t, MatchPhase(3), PhaseTeleop)
	assert.Equal(t, MatchPhase(4), PhaseFinished)
}

func TestNoopFieldEStopPanelImplementsInterface(t *testing.T) {
	var fep FieldEStopPanel = &NoopFieldEStopPanel{}
	state, fault := fep.State()
	assert.Equal(t, StopOK, state)
	assert.Equal(t, FaultNone, fault)
	fep.Clear() // must not panic
	state, _ = fep.State()
	assert.Equal(t, StopOK, state)
}

// --- DecodeChannels truth table ---

func TestDecodeChannelsReleased(t *testing.T) {
	// NC closed (LOW), NO open (HIGH): the healthy released state.
	state, fault := DecodeChannels(0, 1)
	assert.Equal(t, StopOK, state)
	assert.Equal(t, FaultNone, fault)
}

func TestDecodeChannelsPressed(t *testing.T) {
	// NC open (HIGH), NO closed (LOW): the healthy pressed state.
	state, fault := DecodeChannels(1, 0)
	assert.Equal(t, StopActive, state)
	assert.Equal(t, FaultNone, fault)
}

func TestDecodeChannelsBothOpenIsFault(t *testing.T) {
	// Cut conductor or broken common ground: both lines float up to the pull-up.
	state, fault := DecodeChannels(1, 1)
	assert.Equal(t, StopFault, state)
	assert.Equal(t, FaultBothOpen, fault)
}

func TestDecodeChannelsBothClosedIsFault(t *testing.T) {
	// Shorted conductors or a welded contact.
	state, fault := DecodeChannels(0, 0)
	assert.Equal(t, StopFault, state)
	assert.Equal(t, FaultBothClosed, fault)
}

func TestDecodeSingle(t *testing.T) {
	assert.Equal(t, StopOK, DecodeSingle(1))
	assert.Equal(t, StopActive, DecodeSingle(0))
}

func TestInputStateStoppedCoversFaults(t *testing.T) {
	assert.False(t, InputState{State: StopOK}.Stopped())
	assert.True(t, InputState{State: StopActive}.Stopped())
	assert.True(t, InputState{State: StopFault}.Stopped())
}

// --- DiscrepancyFilter ---

func TestDiscrepancyFilterHoldsFaultDuringContactTravel(t *testing.T) {
	f := DiscrepancyFilter{Window: 300 * time.Millisecond}
	start := time.Now()

	// Both channels open mid-press: stop immediately, but do not call it a fault.
	state, fault := f.Update(StopFault, FaultBothOpen, start)
	assert.Equal(t, StopActive, state, "an ambiguous reading must still stop the field")
	assert.Equal(t, FaultNone, fault)

	// Still inside the window.
	state, fault = f.Update(StopFault, FaultBothOpen, start.Add(299*time.Millisecond))
	assert.Equal(t, StopActive, state)
	assert.Equal(t, FaultNone, fault)
}

func TestDiscrepancyFilterRaisesFaultAfterWindow(t *testing.T) {
	f := DiscrepancyFilter{Window: 300 * time.Millisecond}
	start := time.Now()

	f.Update(StopFault, FaultBothOpen, start)
	state, fault := f.Update(StopFault, FaultBothOpen, start.Add(300*time.Millisecond))
	assert.Equal(t, StopFault, state)
	assert.Equal(t, FaultBothOpen, fault)
}

func TestDiscrepancyFilterResetsOnHealthyReading(t *testing.T) {
	f := DiscrepancyFilter{Window: 300 * time.Millisecond}
	start := time.Now()

	// A normal press: brief both-open transit, then a clean pressed reading.
	f.Update(StopFault, FaultBothOpen, start)
	state, _ := f.Update(StopActive, FaultNone, start.Add(20*time.Millisecond))
	assert.Equal(t, StopActive, state)

	// A later mismatch starts a fresh window rather than inheriting the old one.
	state, fault := f.Update(StopFault, FaultBothOpen, start.Add(30*time.Millisecond))
	assert.Equal(t, StopActive, state)
	assert.Equal(t, FaultNone, fault)
}

// --- GpioFieldEStopPanel ---

func TestGpioFieldEStopPanel_HealthyReleased(t *testing.T) {
	panel, _, _ := newTestGpioPanel(0, 1)
	state, fault := panel.State()
	assert.Equal(t, StopOK, state)
	assert.Equal(t, FaultNone, fault)
}

func TestGpioFieldEStopPanel_LatchOnPress(t *testing.T) {
	panel, _, _ := newTestGpioPanel(1, 0)
	state, fault := panel.State()
	assert.Equal(t, StopActive, state)
	assert.Equal(t, FaultNone, fault)
}

func TestGpioFieldEStopPanel_LatchPersistsAfterRelease(t *testing.T) {
	panel, nc, no := newTestGpioPanel(1, 0)
	state, _ := panel.State()
	assert.Equal(t, StopActive, state)

	// Button released — latch must persist until acknowledged.
	nc.val, no.val = 0, 1
	state, _ = panel.State()
	assert.Equal(t, StopActive, state, "latch must remain after button release")
}

func TestGpioFieldEStopPanel_ClearReleasedButton(t *testing.T) {
	panel, nc, no := newTestGpioPanel(1, 0)
	state, _ := panel.State()
	assert.Equal(t, StopActive, state)

	nc.val, no.val = 0, 1
	panel.Clear()
	state, _ = panel.State()
	assert.Equal(t, StopOK, state, "latch should clear once the button reads released")
}

func TestGpioFieldEStopPanel_ClearNoopWhileHeld(t *testing.T) {
	panel, _, _ := newTestGpioPanel(1, 0)
	panel.State() // latch
	panel.Clear()
	state, _ := panel.State()
	assert.Equal(t, StopActive, state, "clear while held must be a no-op")
}

func TestGpioFieldEStopPanel_FaultLatchesAndBlocksClear(t *testing.T) {
	panel, nc, no := newTestGpioPanel(1, 1) // both open: cut conductor
	state, fault := panel.State()
	assert.Equal(t, StopFault, state)
	assert.Equal(t, FaultBothOpen, fault)

	// Clearing while the wiring is still faulted must not work.
	panel.Clear()
	state, _ = panel.State()
	assert.Equal(t, StopFault, state, "a live fault must not be clearable")

	// Repair the wiring, then clear.
	nc.val, no.val = 0, 1
	panel.Clear()
	state, fault = panel.State()
	assert.Equal(t, StopOK, state)
	assert.Equal(t, FaultNone, fault)
}

func TestGpioFieldEStopPanel_FaultOutranksPress(t *testing.T) {
	panel, nc, no := newTestGpioPanel(1, 0) // pressed
	state, _ := panel.State()
	assert.Equal(t, StopActive, state)

	// Wiring then faults; the latch must escalate rather than stay a plain press.
	nc.val, no.val = 0, 0
	state, fault := panel.State()
	assert.Equal(t, StopFault, state)
	assert.Equal(t, FaultBothClosed, fault)

	// Back to a plain press: the fault stays latched.
	nc.val, no.val = 1, 0
	state, _ = panel.State()
	assert.Equal(t, StopFault, state, "a fault must not be downgraded by a later clean reading")
}

func TestGpioFieldEStopPanel_ReadErrorIsFault(t *testing.T) {
	panel, _, no := newTestGpioPanel(0, 1)
	no.err = errors.New("gpio unavailable")
	state, fault := panel.State()
	assert.Equal(t, StopFault, state)
	assert.Equal(t, FaultReadError, fault, "an unreadable line must not hold the previous value")
}

func TestGpioFieldEStopPanel_SingleChannelHasNoFaultDetection(t *testing.T) {
	panel, no := newTestSingleChannelPanel(1)
	state, fault := panel.State()
	assert.Equal(t, StopOK, state)
	assert.Equal(t, FaultNone, fault)

	no.val = 0
	state, fault = panel.State()
	assert.Equal(t, StopActive, state)
	assert.Equal(t, FaultNone, fault)
}

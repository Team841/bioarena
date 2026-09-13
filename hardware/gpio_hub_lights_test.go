package hardware

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// recordingLine is a GPIO output that remembers every value written to it.
type recordingLine struct {
	mu     sync.Mutex
	values []int
	err    error
	closed bool
}

func (l *recordingLine) SetValue(value int) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.err != nil {
		return l.err
	}
	l.values = append(l.values, value)
	return nil
}

func (l *recordingLine) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closed = true
	return nil
}

func (l *recordingLine) written() []int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]int(nil), l.values...)
}

func (l *recordingLine) last() int {
	values := l.written()
	if len(values) == 0 {
		return -1
	}
	return values[len(values)-1]
}

// testPulse keeps the presser quick without making it instantaneous, so a test can
// still catch the trigger in its asserted state.
const testPulse = 5 * time.Millisecond

func newTestHubLights(t *testing.T) (*GpioHubLights, *recordingLine, *recordingLine, *recordingLine) {
	t.Helper()
	trigger, red, blue := &recordingLine{}, &recordingLine{}, &recordingLine{}
	h := newGpioHubLights(trigger, red, blue, testPulse)
	t.Cleanup(h.Close)
	return h, trigger, red, blue
}

// waitForPresses blocks until the trigger has completed the given number of pulses.
func waitForPresses(t *testing.T, trigger *recordingLine, presses int) {
	t.Helper()
	assert.Eventually(t, func() bool {
		return len(trigger.written()) >= 2*presses && trigger.last() == 0
	}, time.Second, time.Millisecond)
}

// A press is an assert followed by a release: the falling edge the sketch is watching for,
// and then the line back where the physical button can use it.
func TestGpioHubLightsPressesTriggerAtMatchStart(t *testing.T) {
	h, trigger, _, _ := newTestHubLights(t)

	assert.NoError(t, h.SetState(LightingState{Phase: PhaseAuto, AutoWinner: AllianceRed}))
	waitForPresses(t, trigger, 1)
	assert.Equal(t, []int{1, 0}, trigger.written())
}

// The winner lines are the toggle switch. Exactly one is asserted, chosen by the AUTO
// result, and it is settled before the press so the sketch cannot sample it half-set.
func TestGpioHubLightsSetsWinnerBeforePressing(t *testing.T) {
	h, trigger, red, blue := newTestHubLights(t)

	assert.NoError(t, h.SetState(LightingState{Phase: PhaseAuto, AutoWinner: AllianceBlue}))
	// The winner is written synchronously; the press is not.
	assert.Equal(t, 0, red.last())
	assert.Equal(t, 1, blue.last())
	waitForPresses(t, trigger, 1)
}

func TestGpioHubLightsRedWinnerAssertsRedLine(t *testing.T) {
	h, _, red, blue := newTestHubLights(t)

	assert.NoError(t, h.SetState(LightingState{Phase: PhaseAuto, AutoWinner: AllianceRed}))
	assert.Equal(t, 1, red.last())
	assert.Equal(t, 0, blue.last())
}

// The shifts, the pause, the warnings: all the sketch's own business. Nothing is driven.
func TestGpioHubLightsIgnoresMidMatchTransitions(t *testing.T) {
	h, trigger, red, blue := newTestHubLights(t)
	assert.NoError(t, h.SetState(LightingState{Phase: PhaseAuto, AutoWinner: AllianceRed}))
	waitForPresses(t, trigger, 1)

	for _, state := range []LightingState{
		{Phase: PhaseAuto, AutoWinner: AllianceRed, ShiftWarning: true},
		{Phase: PhasePause, AutoWinner: AllianceRed},
		{Phase: PhaseTeleop, AutoWinner: AllianceRed},
		{Phase: PhaseTeleop, AutoWinner: AllianceRed, ShiftWarning: true},
	} {
		assert.NoError(t, h.SetState(state))
	}
	time.Sleep(4 * testPulse)
	assert.Equal(t, []int{1, 0}, trigger.written(), "no second press")
	assert.Equal(t, []int{1}, red.written(), "winner written once")
	assert.Equal(t, []int{0}, blue.written())
}

// The Arduino's clock ends its own match. A press then would start the next one.
func TestGpioHubLightsLeavesTriggerAloneAtNaturalEnd(t *testing.T) {
	h, trigger, red, blue := newTestHubLights(t)
	assert.NoError(t, h.SetState(LightingState{Phase: PhaseAuto, AutoWinner: AllianceRed}))
	waitForPresses(t, trigger, 1)

	assert.NoError(t, h.SetState(LightingState{Phase: PhaseFinished, AutoWinner: AllianceRed}))
	time.Sleep(4 * testPulse)
	assert.Equal(t, []int{1, 0}, trigger.written())
	// The switch is let go of, ready for the next match's result.
	assert.Equal(t, 0, red.last())
	assert.Equal(t, 0, blue.last())
}

// An abort is the one end the Arduino cannot see for itself; the button has to be pressed
// again to stop it, exactly as an operator would.
func TestGpioHubLightsPressesAgainOnAbort(t *testing.T) {
	h, trigger, red, _ := newTestHubLights(t)
	assert.NoError(t, h.SetState(LightingState{Phase: PhaseAuto, AutoWinner: AllianceRed}))
	waitForPresses(t, trigger, 1)

	assert.NoError(t, h.SetState(LightingState{Phase: PhaseFinished, Aborted: true}))
	waitForPresses(t, trigger, 2)
	assert.Equal(t, []int{1, 0, 1, 0}, trigger.written())
	assert.Equal(t, 0, red.last())
}

// An abort before AUTO -- from the warmup -- never started the Arduino, so there is
// nothing to stop, and a press would start one.
func TestGpioHubLightsDoesNotPressForAnAbortBeforeStart(t *testing.T) {
	h, trigger, _, _ := newTestHubLights(t)

	assert.NoError(t, h.SetState(LightingState{Phase: PhaseIdle}))
	assert.NoError(t, h.SetState(LightingState{Phase: PhaseFinished, Aborted: true}))
	time.Sleep(4 * testPulse)
	assert.Empty(t, trigger.written())
}

// Two presses queued back to back must come out as two edges, with the line released in
// between, or the sketch sees one long press.
func TestGpioHubLightsSeparatesConsecutivePresses(t *testing.T) {
	h, trigger, _, _ := newTestHubLights(t)

	assert.NoError(t, h.SetState(LightingState{Phase: PhaseAuto, AutoWinner: AllianceRed}))
	assert.NoError(t, h.SetState(LightingState{Phase: PhaseFinished, Aborted: true}))
	waitForPresses(t, trigger, 2)
	assert.Equal(t, []int{1, 0, 1, 0}, trigger.written())
}

// The press runs on its own goroutine and must not surface as a SetState error, but a
// winner line that cannot be driven is reported, since the match is about to start on a
// wrong result.
func TestGpioHubLightsReportsWinnerLineErrors(t *testing.T) {
	trigger, red, blue := &recordingLine{}, &recordingLine{err: errors.New("EIO")}, &recordingLine{}
	h := newGpioHubLights(trigger, red, blue, testPulse)
	t.Cleanup(h.Close)

	err := h.SetState(LightingState{Phase: PhaseAuto, AutoWinner: AllianceRed})
	assert.ErrorContains(t, err, "red-wins")
	time.Sleep(4 * testPulse)
	assert.Empty(t, trigger.written(), "the match must not be started on a wrong result")
}

// A field wired with only the blue line still gets both outcomes: the sketch reads
// neither-asserted as red.
func TestGpioHubLightsWorksWithOnlyTheBlueLine(t *testing.T) {
	trigger, blue := &recordingLine{}, &recordingLine{}
	h := newGpioHubLights(trigger, nil, blue, testPulse)
	t.Cleanup(h.Close)

	assert.NoError(t, h.SetState(LightingState{Phase: PhaseAuto, AutoWinner: AllianceRed}))
	assert.Equal(t, 0, blue.last())
	waitForPresses(t, trigger, 1)

	assert.NoError(t, h.SetState(LightingState{Phase: PhaseFinished}))
	assert.NoError(t, h.SetState(LightingState{Phase: PhaseAuto, AutoWinner: AllianceBlue}))
	assert.Equal(t, 1, blue.last())
}

// Close waits for a press in flight, releases every line, and hands them back, so the
// replacement built on the next settings save can open the same pins.
func TestGpioHubLightsCloseReleasesAndClosesLines(t *testing.T) {
	trigger, red, blue := &recordingLine{}, &recordingLine{}, &recordingLine{}
	h := newGpioHubLights(trigger, red, blue, testPulse)

	assert.NoError(t, h.SetState(LightingState{Phase: PhaseAuto, AutoWinner: AllianceRed}))
	h.Close()
	h.Close() // idempotent

	assert.Equal(t, 0, trigger.last(), "trigger released")
	assert.Equal(t, 0, red.last(), "winner released")
	assert.True(t, trigger.closed)
	assert.True(t, red.closed)
	assert.True(t, blue.closed)
	assert.Error(t, h.SetState(LightingState{Phase: PhaseAuto}), "a closed driver drives nothing")
}

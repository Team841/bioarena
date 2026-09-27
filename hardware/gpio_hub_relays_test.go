package hardware

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/team841/bioarena/led"
)

// recordingLine is a GPIO output that remembers every value written to it.
type recordingLine struct {
	values []int
	err    error
	closed bool
}

func (l *recordingLine) SetValue(value int) error {
	if l.err != nil {
		return l.err
	}
	l.values = append(l.values, value)
	return nil
}

func (l *recordingLine) Close() error {
	l.closed = true
	return nil
}

func (l *recordingLine) last() int {
	if len(l.values) == 0 {
		return -1
	}
	return l.values[len(l.values)-1]
}

// fakeClock is a clock the tests advance by hand.
type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time                { return c.now }
func (c *fakeClock) Advance(elapsed time.Duration) { c.now = c.now.Add(elapsed) }

func newTestRelays() (*GpioHubRelays, *recordingLine, *recordingLine) {
	relays, red, blue, _ := newTestRelaysWithClock()
	return relays, red, blue
}

func newTestRelaysWithClock() (*GpioHubRelays, *recordingLine, *recordingLine, *fakeClock) {
	red, blue := &recordingLine{}, &recordingLine{}
	clock := &fakeClock{now: time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)}
	relays := newGpioHubRelays(red, blue)
	relays.now = clock.Now
	return relays, red, blue, clock
}

// Each relay follows its own HUB's mode: lit for a steady mode, dark for Off.
func TestGpioHubRelaysFollowModes(t *testing.T) {
	relays, red, blue := newTestRelays()

	relays.SetMode(led.RedMode, led.OffMode)
	assert.Nil(t, relays.Update())
	assert.Equal(t, 1, red.last())
	assert.Equal(t, 0, blue.last())

	relays.SetMode(led.OffMode, led.BlueMode)
	assert.Nil(t, relays.Update())
	assert.Equal(t, 0, red.last())
	assert.Equal(t, 1, blue.last())
}

// Nothing is driven until Update, and the first Update drives both lines even when they are
// already dark: the relay board's state before bioarena claimed the pins is not known.
func TestGpioHubRelaysFirstUpdateDrivesBothLines(t *testing.T) {
	relays, red, blue := newTestRelays()
	assert.Empty(t, red.values)

	assert.Nil(t, relays.Update())
	assert.Equal(t, []int{0}, red.values)
	assert.Equal(t, []int{0}, blue.values)
}

// Update runs at 100 Hz. A relay clicked a hundred times a second would not last a match,
// so a line is only written when its lamp changes -- not when a mode changes to another
// steady mode that lights the same lamp, and not when the other lamp changes.
func TestGpioHubRelaysWriteOnlyOnChange(t *testing.T) {
	relays, red, blue := newTestRelays()
	relays.SetMode(led.RedStartupMode, led.BlueStartupMode)
	for i := 0; i < 50; i++ {
		assert.Nil(t, relays.Update())
	}
	relays.SetMode(led.RedMode, led.BlueMode)
	assert.Nil(t, relays.Update())
	relays.SetMode(led.RedMode, led.OffMode)
	assert.Nil(t, relays.Update())

	assert.Equal(t, []int{1}, red.values)
	assert.Equal(t, []int{1, 0}, blue.values)
}

// The warning pulse is game information, so the relay blinks it: dark at once, then lit,
// then dark, at the pulse's own rhythm. The other HUB holds steady throughout.
func TestGpioHubRelaysBlinkThePulse(t *testing.T) {
	relays, red, blue, clock := newTestRelaysWithClock()
	relays.SetMode(led.RedMode, led.BlueMode)
	assert.Nil(t, relays.Update())

	relays.SetMode(led.RedMode, led.BluePulseMode)
	assert.Nil(t, relays.Update())
	assert.Equal(t, 0, blue.last(), "the warning starts dark, so it is seen at once")

	clock.Advance(led.PulseBlinkHalfPeriod)
	relays.SetMode(led.RedMode, led.BluePulseMode)
	assert.Nil(t, relays.Update())
	assert.Equal(t, 1, blue.last())

	clock.Advance(led.PulseBlinkHalfPeriod)
	relays.SetMode(led.RedMode, led.BluePulseMode)
	assert.Nil(t, relays.Update())
	assert.Equal(t, 0, blue.last())

	assert.Equal(t, []int{1}, red.values, "the steady HUB is not touched")
}

// The arena sets the modes on every tick. Re-setting the mode a blink is already in must not
// restart it, or the lamp would sit dark for as long as the warning lasts.
func TestGpioHubRelaysBlinkSurvivesRepeatedSetMode(t *testing.T) {
	relays, _, blue, clock := newTestRelaysWithClock()
	relays.SetMode(led.OffMode, led.BluePulseMode)
	for elapsed := time.Duration(0); elapsed <= led.PulseBlinkHalfPeriod; elapsed += 10 * time.Millisecond {
		relays.SetMode(led.OffMode, led.BluePulseMode)
		assert.Nil(t, relays.Update())
		clock.Advance(10 * time.Millisecond)
	}
	assert.Equal(t, []int{0, 1}, blue.values)
}

// A write that fails is not remembered as having landed, so the next tick tries again.
func TestGpioHubRelaysRetryAfterFailedWrite(t *testing.T) {
	relays, red, _ := newTestRelays()
	relays.SetMode(led.RedMode, led.OffMode)

	red.err = errors.New("EIO")
	assert.ErrorContains(t, relays.Update(), "red")

	red.err = nil
	assert.Nil(t, relays.Update())
	assert.Equal(t, 1, red.last())
}

// Close darkens both lamps and releases the lines, so the output rebuilt on the next settings
// save can open the same pins.
func TestGpioHubRelaysCloseDarkensAndReleases(t *testing.T) {
	relays, red, blue := newTestRelays()
	relays.SetMode(led.RedMode, led.BlueMode)
	assert.Nil(t, relays.Update())

	relays.Close()
	relays.Close() // idempotent

	assert.Equal(t, 0, red.last())
	assert.Equal(t, 0, blue.last())
	assert.True(t, red.closed)
	assert.True(t, blue.closed)

	// A closed driver drives nothing, rather than writing to lines it no longer holds.
	writes := len(red.values)
	relays.SetMode(led.RedMode, led.BlueMode)
	assert.Nil(t, relays.Update())
	assert.Len(t, red.values, writes)
}

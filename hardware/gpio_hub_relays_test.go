package hardware

import (
	"errors"
	"testing"

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

func newTestRelays() (*GpioHubRelays, *recordingLine, *recordingLine) {
	red, blue := &recordingLine{}, &recordingLine{}
	return newGpioHubRelays(red, blue), red, blue
}

// Each relay follows its own HUB's mode: lit for anything but Off.
func TestGpioHubRelaysFollowModes(t *testing.T) {
	relays, red, blue := newTestRelays()

	relays.SetMode(led.RedMode, led.OffMode)
	assert.Nil(t, relays.Update())
	assert.Equal(t, 1, red.last())
	assert.Equal(t, 0, blue.last())

	relays.SetMode(led.OffMode, led.BluePulseMode)
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
// so the lines are only written when a lamp changes -- not when a mode changes to another
// mode that lights the same lamp.
func TestGpioHubRelaysWriteOnlyOnChange(t *testing.T) {
	relays, red, _ := newTestRelays()
	relays.SetMode(led.RedStartupMode, led.BlueStartupMode)
	for i := 0; i < 50; i++ {
		assert.Nil(t, relays.Update())
	}
	relays.SetMode(led.RedMode, led.BlueMode)
	assert.Nil(t, relays.Update())
	relays.SetMode(led.RedPulseMode, led.BlueMode)
	assert.Nil(t, relays.Update())

	assert.Equal(t, []int{1}, red.values)
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

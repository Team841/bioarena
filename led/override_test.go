package led

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Off is the only mode that is always dark, and the startup fill, which carries no game
// information and ends solid, is steady on like the plain alliance colours.
func TestLitSteadyModes(t *testing.T) {
	for _, elapsed := range []time.Duration{0, 350 * time.Millisecond, 5 * time.Second} {
		assert.False(t, Lit(OffMode, elapsed))
		for _, mode := range []Mode{RedMode, BlueMode, WhiteMode, RedStartupMode, BlueStartupMode} {
			assert.True(t, Lit(mode, elapsed), "mode %d at %v", mode, elapsed)
		}
	}
}

// The pulse is the warning that a HUB is about to go inactive, so on a lamp that cannot dim
// it blinks rather than holding steady. It keeps the DMX pulse's rhythm and starts dark.
func TestLitPulseBlinks(t *testing.T) {
	assert.Equal(t, 700*time.Millisecond, PulseBlinkHalfPeriod)
	for _, mode := range []Mode{RedPulseMode, BluePulseMode} {
		assert.False(t, Lit(mode, 0), "starts dark")
		assert.False(t, Lit(mode, 699*time.Millisecond))
		assert.True(t, Lit(mode, 700*time.Millisecond))
		assert.True(t, Lit(mode, 1399*time.Millisecond))
		assert.False(t, Lit(mode, 1400*time.Millisecond))
	}
}

// The advantage sweep marks the AUTO winner's HUB during the transition shift. It blinks
// once per pass of the sweep, faster than the pulse, so the two remain tellable apart.
func TestLitAdvantageBlinksFasterThanPulse(t *testing.T) {
	assert.Equal(t, 360*time.Millisecond, AdvantageBlinkHalfPeriod)
	assert.Less(t, AdvantageBlinkHalfPeriod, PulseBlinkHalfPeriod)
	for _, mode := range []Mode{RedAdvantageMode, BlueAdvantageMode} {
		assert.False(t, Lit(mode, 0), "starts dark")
		assert.True(t, Lit(mode, 360*time.Millisecond))
		assert.False(t, Lit(mode, 720*time.Millisecond))
	}
}

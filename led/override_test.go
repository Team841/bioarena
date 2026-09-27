package led

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// A relay has one question to answer per HUB: on or off. Off is the only mode that says off;
// the sequences that animate or pulse on DMX fixtures are all still an active HUB.
func TestLit(t *testing.T) {
	assert.False(t, Lit(OffMode))
	for _, mode := range []Mode{
		RedMode, BlueMode, WhiteMode,
		RedStartupMode, BlueStartupMode,
		RedAdvantageMode, BlueAdvantageMode,
		RedPulseMode, BluePulseMode,
	} {
		assert.True(t, Lit(mode), "mode %d", mode)
	}
}

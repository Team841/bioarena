package plc

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// fakeHubCountSource stands in for the counter receiver.
type fakeHubCountSource struct {
	red, blue int
	resets    int
}

func (source *fakeHubCountSource) GetHubCounts() (int, int) {
	return source.red, source.blue
}

func (source *fakeHubCountSource) ResetMatch() {
	source.resets++
	source.red, source.blue = 0, 0
}

// countingFakePlc records that the base was reached, which is how the tests below tell
// delegation from a wrapper that swallowed the call.
type countingFakePlc struct {
	FakePlc
	resets int
}

func (plc *countingFakePlc) ResetMatch() {
	plc.resets++
}

func TestWithHubCountsAnswersFromTheSource(t *testing.T) {
	source := &fakeHubCountSource{red: 12, blue: 5}
	plc := WithHubCounts(new(FakePlc), source)

	red, blue := plc.GetHubCounts()
	assert.Equal(t, 12, red)
	assert.Equal(t, 5, blue)

	source.red, source.blue = 13, 5
	red, blue = plc.GetHubCounts()
	assert.Equal(t, 13, red)
	assert.Equal(t, 5, blue)
}

// The wrapper has to be a Plc in full: the arena calls two dozen other methods on it
// every loop, and an embedded interface is what keeps them the base's own.
func TestWithHubCountsDelegatesEverythingElse(t *testing.T) {
	base := new(FakePlc)
	base.SetAddress("")
	plc := WithHubCounts(base, &fakeHubCountSource{})

	assert.Equal(t, base.IsEnabled(), plc.IsEnabled())
	assert.Equal(t, base.IsHealthy(), plc.IsHealthy())
	assert.Equal(t, base.GetFieldEStop(), plc.GetFieldEStop())
	assert.Equal(t, base.GetArmorBlockStatuses(), plc.GetArmorBlockStatuses())
	assert.Equal(t, base.GetInputNames(), plc.GetInputNames())
	assert.Equal(t, base.GetRegisterNames(), plc.GetRegisterNames())
	assert.Equal(t, base.GetCoilNames(), plc.GetCoilNames())
	assert.Equal(t, base.IoChangeNotifier(), plc.IoChangeNotifier())

	baseRedEStops, baseBlueEStops := base.GetTeamEStops()
	redEStops, blueEStops := plc.GetTeamEStops()
	assert.Equal(t, baseRedEStops, redEStops)
	assert.Equal(t, baseBlueEStops, blueEStops)

	// Setters must reach the base rather than vanishing into the wrapper.
	plc.SetStackLights(true, true, true, true)
	plc.SetStackBuzzer(true)
	plc.SetFieldResetLight(true)
}

// ResetMatch is what takes the counter's baseline, and it is also what the base needs to
// start a match. Reaching only one of them would break the other.
func TestWithHubCountsResetsBoth(t *testing.T) {
	base := new(countingFakePlc)
	source := &fakeHubCountSource{red: 40, blue: 9}
	plc := WithHubCounts(base, source)

	plc.ResetMatch()

	assert.Equal(t, 1, base.resets, "the base PLC still has a match to reset")
	assert.Equal(t, 1, source.resets, "the counter needs the signal to take its baseline")

	red, blue := plc.GetHubCounts()
	assert.Equal(t, 0, red)
	assert.Equal(t, 0, blue)
}

// A field with no counter configured must be left with exactly the Plc it had, not a
// wrapper reporting nothing.
func TestWithHubCountsWithoutASourceReturnsTheBase(t *testing.T) {
	base := new(FakePlc)

	plc := WithHubCounts(base, nil)

	assert.Same(t, base, plc)
	red, blue := plc.GetHubCounts()
	assert.Equal(t, 0, red)
	assert.Equal(t, 0, blue)
}

// The registers restored from upstream have to be the ones GetHubCounts reads, or a real
// PLC would report FUEL from whichever word happened to land there.
func TestModbusPlcReadsTheHubTotalRegisters(t *testing.T) {
	plc := new(ModbusPlc)
	plc.registers[redHubTotal] = 37
	plc.registers[blueHubTotal] = 21

	red, blue := plc.GetHubCounts()
	assert.Equal(t, 37, red)
	assert.Equal(t, 21, blue)

	// And clearing the mirror is what makes the counts match-relative.
	plc.ResetMatch()
	red, blue = plc.GetHubCounts()
	assert.Equal(t, 0, red)
	assert.Equal(t, 0, blue)
}

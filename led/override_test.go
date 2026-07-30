package led

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSetLayoutSingleFixturePerAlliance(t *testing.T) {
	controller := NewController()
	err := controller.SetLayout(
		[]FixtureSpec{{Universe: 1, StartAddress: 1}},
		[]FixtureSpec{{Universe: 1, StartAddress: 25}},
	)
	assert.Nil(t, err)
	assert.Len(t, controller.fixtures.red, 1)
	assert.Len(t, controller.fixtures.blue, 1)
	assert.Equal(t, 25, controller.fixtures.blue[0].startAddress)
}

// A single-pixel fixture reads the first three channels of the block the renderer
// writes, so a solid mode must land its colour there.
func TestSingleFixtureLayoutEmitsAllianceColour(t *testing.T) {
	controller := NewController()
	assert.Nil(t, controller.SetLayout(
		[]FixtureSpec{{Universe: 1, StartAddress: 1}},
		[]FixtureSpec{{Universe: 1, StartAddress: 25}},
	))
	controller.SetMode(RedMode, BlueMode)
	controller.redZone.updatePixels(Red)
	controller.blueZone.updatePixels(Blue)
	assert.Nil(t, controller.populateFixtureData(&controller.redZone, controller.fixtures.red))
	assert.Nil(t, controller.populateFixtureData(&controller.blueZone, controller.fixtures.blue))

	data := controller.universes[1].currentData
	// Red fixture at address 1 occupies channels 1-3.
	assert.Equal(t, [3]byte{255, 0, 0}, [3]byte{data[0], data[1], data[2]})
	// Blue fixture at address 25 occupies channels 25-27.
	assert.Equal(t, [3]byte{0, 0, 255}, [3]byte{data[24], data[25], data[26]})
}

func TestSetLayoutRejectsOverlappingFixtures(t *testing.T) {
	controller := NewController()
	// 24 channels are written per fixture, so 1 and 4 collide.
	err := controller.SetLayout(
		[]FixtureSpec{{Universe: 1, StartAddress: 1}},
		[]FixtureSpec{{Universe: 1, StartAddress: 4}},
	)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "overlap")

	// The same addresses in different universes are fine.
	assert.Nil(t, controller.SetLayout(
		[]FixtureSpec{{Universe: 1, StartAddress: 1}},
		[]FixtureSpec{{Universe: 2, StartAddress: 1}},
	))
}

func TestSetLayoutRejectsInvalidAddresses(t *testing.T) {
	controller := NewController()

	assert.NotNil(t, controller.SetLayout([]FixtureSpec{{Universe: 0, StartAddress: 1}}, nil))
	assert.NotNil(t, controller.SetLayout([]FixtureSpec{{Universe: 1, StartAddress: 0}}, nil))
	// A fixture needs a full 24-channel block inside the 512-channel universe.
	assert.NotNil(t, controller.SetLayout([]FixtureSpec{{Universe: 1, StartAddress: 500}}, nil))
	assert.Nil(t, controller.SetLayout([]FixtureSpec{{Universe: 1, StartAddress: 489}}, nil))
}

func TestUseDefaultLayoutRestoresFullField(t *testing.T) {
	controller := NewController()
	assert.Nil(t, controller.SetLayout([]FixtureSpec{{Universe: 1, StartAddress: 1}}, nil))
	assert.Len(t, controller.fixtures.red, 1)

	controller.UseDefaultLayout()
	assert.Len(t, controller.fixtures.red, 8)
	assert.Len(t, controller.fixtures.blue, 8)
}

func TestParseFallback(t *testing.T) {
	for name, expected := range map[string]Fallback{
		"full":   FallbackFull,
		"solid":  FallbackSolid,
		"binary": FallbackBinary,
	} {
		fallback, err := ParseFallback(name)
		assert.Nil(t, err)
		assert.Equal(t, expected, fallback)
		assert.Equal(t, name, fallback.String())
	}

	_, err := ParseFallback("sparkly")
	assert.NotNil(t, err)
}

func TestApplyFallbackFullPassesEverythingThrough(t *testing.T) {
	for _, mode := range []Mode{RedStartupMode, RedAdvantageMode, RainbowMode, RedPulseMode, OffMode} {
		assert.Equal(t, mode, ApplyFallback(FallbackFull, mode, RedMode))
	}
}

// Per-pixel sequences collapse to the zone's own colour, so the blue zone must not end
// up red.
func TestApplyFallbackSolidUsesZoneColour(t *testing.T) {
	perPixel := []Mode{
		RedStartupMode, BlueStartupMode, RedAdvantageMode, BlueAdvantageMode,
		RainbowMode, Side1TestMode, Side2TestMode, Side3TestMode, Side4TestMode,
	}
	for _, mode := range perPixel {
		assert.Equal(t, RedMode, ApplyFallback(FallbackSolid, mode, RedMode), "mode %v red zone", mode)
		assert.Equal(t, BlueMode, ApplyFallback(FallbackSolid, mode, BlueMode), "mode %v blue zone", mode)
	}

	// Pulses vary brightness uniformly, which a dimmable single-pixel fixture renders
	// correctly, so solid keeps them.
	assert.Equal(t, RedPulseMode, ApplyFallback(FallbackSolid, RedPulseMode, RedMode))
	assert.Equal(t, BluePulseMode, ApplyFallback(FallbackSolid, BluePulseMode, BlueMode))

	// Off and the plain colours are already renderable.
	assert.Equal(t, OffMode, ApplyFallback(FallbackSolid, OffMode, RedMode))
	assert.Equal(t, WhiteMode, ApplyFallback(FallbackSolid, WhiteMode, RedMode))
}

func TestApplyFallbackBinaryFlattensPulses(t *testing.T) {
	assert.Equal(t, RedMode, ApplyFallback(FallbackBinary, RedPulseMode, RedMode))
	assert.Equal(t, BlueMode, ApplyFallback(FallbackBinary, BluePulseMode, BlueMode))
	assert.Equal(t, RedMode, ApplyFallback(FallbackBinary, RedStartupMode, RedMode))
	assert.Equal(t, OffMode, ApplyFallback(FallbackBinary, OffMode, RedMode))
}

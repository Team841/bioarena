package hardware

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

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
	s1 := LightingState{Phase: PhaseTeleop, TeleopSubPhase: SubPhaseShift1, AutoWinner: AllianceRed, ShiftWarning: false}
	s2 := LightingState{Phase: PhaseTeleop, TeleopSubPhase: SubPhaseShift1, AutoWinner: AllianceRed, ShiftWarning: false}
	s3 := LightingState{Phase: PhaseTeleop, TeleopSubPhase: SubPhaseShift1, AutoWinner: AllianceRed, ShiftWarning: true}

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

func TestTeleopSubPhaseConstants(t *testing.T) {
	assert.Equal(t, TeleopSubPhase(0), SubPhaseNone)
	assert.Equal(t, TeleopSubPhase(1), SubPhaseTransition)
	assert.Equal(t, TeleopSubPhase(2), SubPhaseShift1)
	assert.Equal(t, TeleopSubPhase(3), SubPhaseShift2)
	assert.Equal(t, TeleopSubPhase(4), SubPhaseShift3)
	assert.Equal(t, TeleopSubPhase(5), SubPhaseShift4)
	assert.Equal(t, TeleopSubPhase(6), SubPhaseEndGame)
}

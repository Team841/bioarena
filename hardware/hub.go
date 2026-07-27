package hardware

import "github.com/team841/bioarena/game"

// Shift maps a lighting sub-phase onto upstream's shift enum. The two enums exist
// separately because LightingState is the driver-facing type and predates the port of
// upstream's game/hub.go; this is the single place they are reconciled.
func (subPhase TeleopSubPhase) Shift() game.Shift {
	switch subPhase {
	case SubPhaseTransition:
		return game.ShiftTransition
	case SubPhaseShift1:
		return game.Shift1
	case SubPhaseShift2:
		return game.Shift2
	case SubPhaseShift3:
		return game.Shift3
	case SubPhaseShift4:
		return game.Shift4
	case SubPhaseEndGame:
		return game.ShiftEndgame
	default:
		return game.ShiftCount // not a valid shift
	}
}

// HubActive reports whether the given alliance's HUB is lit for the current state.
//
// The rule itself lives in game.Hub.IsShiftActive, ported from upstream: both HUBs are
// active during AUTO, the transition shift, and endgame; during the alliance shifts the
// AUTO winner's HUB is dark for shifts 1 and 3, its opponent's for shifts 2 and 4.
//
// Lighting drivers must call this rather than reimplementing the alternation, so the
// serial and DMX drivers cannot drift apart.
func HubActive(state LightingState, alliance Alliance) bool {
	hub := &game.Hub{WonAuto: alliance == state.AutoWinner}

	switch state.Phase {
	case PhaseAuto:
		return hub.IsShiftActive(game.ShiftAuto)
	case PhaseTeleop:
		return hub.IsShiftActive(state.TeleopSubPhase.Shift())
	case PhaseFinished:
		return hub.IsShiftActive(game.ShiftPostMatch)
	default:
		// Idle and the auto/teleop pause are not scoring shifts; the HUB is dark.
		return false
	}
}

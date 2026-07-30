package hardware

import "github.com/team841/bioarena/game"

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
	return hub.IsShiftActive(state.Shift)
}

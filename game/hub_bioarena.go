// Portions Copyright Team 841. All Rights Reserved.
//
// Fork-local additions to the 2026 Hub.
//
// Separate from hub.go so that file can be taken from upstream byte-identically -- the
// shift alternation and the FUEL attribution are rules of the game, and a fork that
// edits them in place is a fork that drifts away from them. See
// docs/upstream-divergences.md.

package game

// IsShiftActive reports whether the Hub is lit for the given shift.
//
// Exported only because bioarena's lighting drivers live outside this package, where
// upstream's callers are all inside it. The rule is upstream's isShiftActive, untouched.
func (hub *Hub) IsShiftActive(shift Shift) bool {
	return hub.isShiftActive(shift)
}

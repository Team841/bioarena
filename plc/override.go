// Portions Copyright Team 841. All Rights Reserved.
//
// Practice-field substitution for the PLC's Hub FUEL counters.
//
// Upstream reads scoring from one source in one call -- Plc.GetHubCounts, red and blue
// together -- and hands each number to its own game.Hub. A practice field has no PLC,
// but it can have a webcam counter that reports the same two numbers, so the counter is
// substituted behind that same call rather than the arena growing a second way to score.
//
// Additive, on the pattern of led/override.go: plc.go stays as it came from upstream and
// the fork-local behaviour lives here. See docs/spec-hub-fuel-counter-feed.md.

package plc

// HubCountSource is anything that can stand in for the PLC's Hub counters.
//
// Structural, so the implementation need not live in or import this package: the
// counter receiver is in hardware/, which knows nothing about Modbus.
type HubCountSource interface {
	// GetHubCounts returns the FUEL counted into the red and blue HUBs since the last
	// ResetMatch -- match-relative, which is what the PLC's registers hold once its
	// mirror has been cleared, and what game.Hub.UpdateState expects.
	GetHubCounts() (int, int)

	// ResetMatch makes the counts read zero from here, as clearing the PLC's register
	// mirror does.
	ResetMatch()
}

// WithHubCounts returns a Plc that behaves as base in every respect except that
// GetHubCounts is answered by source, and ResetMatch reaches both.
//
// A nil source returns base untouched, so a field with no counter configured is left
// with exactly the Plc it had rather than a wrapper that reports nothing.
func WithHubCounts(base Plc, source HubCountSource) Plc {
	if source == nil {
		return base
	}
	return &hubCountOverride{Plc: base, source: source}
}

// hubCountOverride embeds the Plc interface, so every method it does not name is the
// base's own. Two are named.
type hubCountOverride struct {
	Plc
	source HubCountSource
}

func (override *hubCountOverride) GetHubCounts() (int, int) {
	return override.source.GetHubCounts()
}

// ResetMatch resets both. The base still has a match to reset -- the matchReset coil,
// the register mirror -- and the source needs the same signal to take its baseline, which
// is the whole reason the arena needs no new call at the start of a match.
func (override *hubCountOverride) ResetMatch() {
	override.Plc.ResetMatch()
	override.source.ResetMatch()
}

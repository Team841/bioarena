// Portions Copyright Team 841. All Rights Reserved.
//
// Practice-field overrides for the upstream Hub LED controller.
//
// This file is a bioarena addition. The ported files in this package are byte-identical
// copies of upstream cheesy-arena, so that `git diff upstream/main -- led/` shows only
// bioarena's own files and upstream lighting changes can be taken with a checkout. Keep it
// that way: put local behaviour here rather than editing the ported files.

package led

import "time"

// counterCycle is how often upstream's zone counters advance: once per arena loop, every
// 10 ms. Upstream times its sequences in counter steps, so this turns them into durations.
const counterCycle = 10 * time.Millisecond

// Blink half-periods for a lamp that can only be switched on or off. Each keeps the rhythm of
// the sequence it stands in for, read from upstream's own constants so a retune there carries
// over: the pulse's rise-and-fall, and one pass of the advantage sweep. The two stay distinct,
// so the last three seconds of the transition shift -- where the AUTO winner's HUB goes from
// sweep to pulse -- still read as a warning.
const (
	PulseBlinkHalfPeriod     = pulseHalfPeriod * counterCycle
	AdvantageBlinkHalfPeriod = (pixelsPerFixture*2 + 2) * advantageStepCycle * counterCycle / 2
)

// Lit reports whether a HUB lamp that can only be switched on or off, such as one run through
// a relay, is lit for a mode that has been running for the given time.
//
// The sequences that carry game information become blinks, because a lamp held steady would
// lose it: the pulse is the warning that a HUB is about to go inactive, and the advantage
// sweep marks the AUTO winner's HUB -- the one that goes inactive first -- during the
// transition shift. A blink starts dark, so the change is visible the moment it begins, as
// upstream's pulse starts from black.
//
// The startup fill is steady on. It runs once, on both HUBs alike, and ends solid.
func Lit(mode Mode, elapsed time.Duration) bool {
	switch mode {
	case OffMode:
		return false
	case RedPulseMode, BluePulseMode:
		return blinkLit(elapsed, PulseBlinkHalfPeriod)
	case RedAdvantageMode, BlueAdvantageMode:
		return blinkLit(elapsed, AdvantageBlinkHalfPeriod)
	default:
		return true
	}
}

// blinkLit is a square wave that starts dark: off for the first half-period, then on.
func blinkLit(elapsed, halfPeriod time.Duration) bool {
	return (elapsed/halfPeriod)%2 == 1
}

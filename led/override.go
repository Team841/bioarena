// Portions Copyright Team 841. All Rights Reserved.
//
// Practice-field overrides for the upstream Hub LED controller.
//
// This file is a bioarena addition. The ported files in this package are byte-identical
// copies of upstream cheesy-arena, so that `git diff upstream/main -- led/` shows only
// bioarena's own files and upstream lighting changes can be taken with a checkout. Keep it
// that way: put local behaviour here rather than editing the ported files.

package led

// Lit reports whether a mode lights a HUB lamp that can only be switched on or off, such
// as one run through a relay. Every mode but Off does.
//
// The relay output follows the same mode sequence the DMX fixtures do, so the two cannot
// disagree about which HUB is active. What a relay loses is only what it cannot show: the
// startup fill, the advantage sweep and the pre-deactivation pulse all read as steady on,
// and the end-of-match white as both lamps lit until the sequence turns them off.
func Lit(mode Mode) bool {
	return mode != OffMode
}

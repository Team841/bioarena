// Portions Copyright Team 841. All Rights Reserved.
//
// Per-match record of the FUEL each HUB took, written when a match is cleared.
//
// A practice field's point is the review afterwards, and the two numbers worth reviewing
// are not on the scoreboard: which shift the FUEL went in, and how much of it went into a
// HUB that was dark. Both are in the Hubs already -- upstream records FUEL under every
// shift and filters at read time -- so this is a projection, not a second tally.

package field

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/team841/bioarena/game"
	"github.com/team841/bioarena/hardware"
	"github.com/team841/bioarena/model"
)

// writeHubFuelLog writes the match's FUEL breakdown to logs/, beside the driver-station
// logs and served from /logs/ with them.
//
// Nothing is written for a match that saw no FUEL. A field with no counter would otherwise
// leave an identical empty file behind after every practice round, and a directory of
// those is worse than no directory at all.
func (arena *Arena) writeHubFuelLog() {
	redHub, blueHub := arena.hubs()
	if hubTotalCount(redHub) == 0 && hubTotalCount(blueHub) == 0 {
		return
	}

	path, err := hubFuelLogPath(arena.CurrentMatch, time.Now())
	if err != nil {
		log.Printf("Could not write the FUEL log: %v", err)
		return
	}
	contents := formatHubFuelLog(arena.CurrentMatch, redHub, blueHub, arena.AutoWinner, arena.matchAborted)
	if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
		log.Printf("Could not write the FUEL log %s: %v", path, err)
		return
	}
	log.Printf("Wrote the FUEL log to %s", path)
}

// hubFuelLogPath names the file and makes sure the directory exists, matching the
// driver-station logs' naming so a match's files sort together.
func hubFuelLogPath(match *model.Match, now time.Time) (string, error) {
	dir := filepath.Join(model.BaseDir, LogsDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	return filepath.Join(dir, fmt.Sprintf(
		"%s_%s_Match_%s_fuel.csv",
		now.Format("20060102150405"),
		match.Type.String(),
		match.ShortName,
	)), nil
}

// formatHubFuelLog renders the breakdown.
//
// Pure, and separate from the writing, so the format can be tested without a filesystem.
// The leading comment lines carry what a coach opening this wants first -- which match,
// and who took the AUTO advantage -- while leaving the rest a plain four-column CSV.
func formatHubFuelLog(
	match *model.Match, redHub, blueHub *game.Hub, autoWinner hardware.Alliance, aborted bool,
) string {
	redAuto := redHub.GetShiftCount(game.ShiftAuto, true)
	blueAuto := blueHub.GetShiftCount(game.ShiftAuto, true)

	var out []byte
	line := func(format string, args ...any) {
		out = append(out, fmt.Sprintf(format+"\n", args...)...)
	}

	line("# %s", match.LongName)
	line("# AUTO: red %d vs blue %d -> %s", redAuto, blueAuto, autoWinnerLetter(autoWinner))
	if aborted {
		// Said plainly, because the shift tallies of a match that stopped early look just
		// like a quiet one otherwise.
		line("# Match was aborted; the shifts after the abort never ran.")
	}
	line("hub,shift,counted,credited")

	for _, hub := range []struct {
		name string
		hub  *game.Hub
	}{{"red", redHub}, {"blue", blueHub}} {
		for shift := game.ShiftAuto; shift < game.ShiftCount; shift++ {
			line(
				"%s,%s,%d,%d",
				hub.name,
				hubShiftName(shift),
				hub.hub.GetShiftCount(shift, false),
				hub.hub.GetShiftCount(shift, true),
			)
		}
		line("%s,TOTAL,%d,%d", hub.name, hubTotalCount(hub.hub), hubCreditedCount(hub.hub))
	}

	return string(out)
}

// autoWinnerLetter is the AUTO result as the driver stations were told it, so the log and
// the robots agree on what the match was run under.
func autoWinnerLetter(autoWinner hardware.Alliance) string {
	switch autoWinner {
	case hardware.AllianceRed:
		return "R"
	case hardware.AllianceBlue:
		return "B"
	default:
		// A match aborted before the teleop transition never had one settled.
		return "undecided"
	}
}

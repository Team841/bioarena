package field

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/team841/bioarena/game"
	"github.com/team841/bioarena/hardware"
	"github.com/team841/bioarena/model"
)

func testMatch() *model.Match {
	return &model.Match{Type: model.Test, ShortName: "T", LongName: "Test Match"}
}

// hubWithCounts builds a Hub holding the given per-shift tallies, indexed by game.Shift.
func hubWithCounts(wonAuto bool, counts map[game.Shift]int) *game.Hub {
	hub := &game.Hub{WonAuto: wonAuto}
	for shift, count := range counts {
		hub.ShiftCounts[shift] = count
	}
	return hub
}

func TestHubFuelLogRecordsEveryShift(t *testing.T) {
	// Red won AUTO, so red's HUB is dark for shifts 1 and 3.
	redHub := hubWithCounts(true, map[game.Shift]int{
		game.ShiftAuto: 12, game.ShiftTransition: 8, game.Shift1: 5, game.Shift2: 15,
	})
	blueHub := hubWithCounts(false, map[game.Shift]int{game.ShiftAuto: 3, game.Shift1: 9})

	out := formatHubFuelLog(testMatch(), redHub, blueHub, hardware.AllianceRed, false)

	assert.Contains(t, out, "# Test Match")
	assert.Contains(t, out, "# AUTO: red 12 vs blue 3 -> R")
	assert.Contains(t, out, "hub,shift,counted,credited")

	// Every shift has a row for both hubs, whether or not anything went in.
	for _, shift := range []string{"AUTO", "TRANSITION", "SHIFT1", "SHIFT2", "SHIFT3", "SHIFT4",
		"ENDGAME", "POST_MATCH"} {
		assert.Contains(t, out, "red,"+shift+",", "no red row for %s", shift)
		assert.Contains(t, out, "blue,"+shift+",", "no blue row for %s", shift)
	}

	// FUEL into a dark HUB is counted but not credited, which is the whole reason this
	// file is worth keeping.
	assert.Contains(t, out, "red,SHIFT1,5,0", "red's HUB is dark for shift 1")
	assert.Contains(t, out, "red,SHIFT2,15,15")
	assert.Contains(t, out, "blue,SHIFT1,9,9")

	assert.Contains(t, out, "red,TOTAL,40,35")
	assert.Contains(t, out, "blue,TOTAL,12,12")
}

// A match that stopped early reads like a quiet one unless the file says so.
func TestHubFuelLogNamesAnAbort(t *testing.T) {
	hub := hubWithCounts(false, map[game.Shift]int{game.ShiftAuto: 1})

	out := formatHubFuelLog(testMatch(), hub, hub, hardware.AllianceBlue, true)
	assert.Contains(t, out, "aborted")

	out = formatHubFuelLog(testMatch(), hub, hub, hardware.AllianceBlue, false)
	assert.NotContains(t, out, "aborted")
}

// A match aborted before the teleop transition never had a winner settled, and the log
// must not claim one.
func TestHubFuelLogSaysWhenAutoWasNeverDecided(t *testing.T) {
	hub := hubWithCounts(false, map[game.Shift]int{game.ShiftAuto: 2})

	out := formatHubFuelLog(testMatch(), hub, hub, hardware.AllianceNone, true)

	assert.Contains(t, out, "-> undecided")
	assert.NotContains(t, out, "-> R")
	assert.NotContains(t, out, "-> B")
}

// The letter has to be the one the driver stations were given, or the log and the robots
// disagree about what the match was run under.
func TestHubFuelLogUsesTheGameDataLetter(t *testing.T) {
	assert.Equal(t, "R", autoWinnerLetter(hardware.AllianceRed))
	assert.Equal(t, "B", autoWinnerLetter(hardware.AllianceBlue))
	assert.Equal(t, "undecided", autoWinnerLetter(hardware.AllianceNone))
}

// --- Writing ---

// Clearing a match is what writes the file, and it has to happen before the tallies are
// zeroed for the next one.
func TestClearingAMatchWritesTheFuelLog(t *testing.T) {
	arena, fakePlc := startedArena(t)
	logsDir := filepath.Join(model.BaseDir, LogsDir)

	// A name of its own. The filename carries the match's short name and a timestamp to
	// the second, so two tests clearing a match in the same second would otherwise write
	// to the same path and each see the other's file.
	arena.CurrentMatch.ShortName = "FuelWrite"

	fakePlc.redHubCount = 6
	arena.Update()
	assert.Equal(t, 6, hubTotalCount(&arena.RedHub))

	assert.Nil(t, arena.AbortMatch())
	assert.Nil(t, arena.ClearMatch())

	written := findFuelLog(t, logsDir, "FuelWrite")
	t.Cleanup(func() { os.Remove(filepath.Join(logsDir, written)) })

	contents, err := os.ReadFile(filepath.Join(logsDir, written))
	assert.Nil(t, err)
	assert.Contains(t, string(contents), "red,TOTAL,6,6")

	// And the tallies really were zeroed afterwards, so the file is the only copy.
	assert.Equal(t, 0, hubTotalCount(&arena.RedHub))
}

// A field with no counter sees no FUEL, and must not leave an identical empty file behind
// after every practice round.
func TestClearingAMatchWithNoFuelWritesNothing(t *testing.T) {
	arena, _ := startedArena(t)
	logsDir := filepath.Join(model.BaseDir, LogsDir)
	arena.CurrentMatch.ShortName = "FuelNone"

	arena.Update()
	assert.Nil(t, arena.AbortMatch())
	assert.Nil(t, arena.ClearMatch())

	assert.Empty(t, fuelLogsMatching(t, logsDir, "FuelNone"), "no FUEL should mean no file")
}

func TestHubFuelLogPathSortsWithTheDriverStationLogs(t *testing.T) {
	at := time.Date(2026, 10, 7, 17, 45, 12, 0, time.UTC)

	path, err := hubFuelLogPath(testMatch(), at)
	assert.Nil(t, err)

	name := filepath.Base(path)
	assert.True(t, strings.HasPrefix(name, "20261007174512_"), "got %q", name)
	assert.True(t, strings.HasSuffix(name, "_fuel.csv"), "got %q", name)
	assert.Equal(t, filepath.Join(model.BaseDir, LogsDir), filepath.Dir(path))
}

// fuelLogsMatching lists the FUEL logs written for a given match short name. Tests use a
// distinct name each so they can find their own file in a shared directory.
func fuelLogsMatching(t *testing.T, dir, shortName string) []string {
	t.Helper()
	var names []string
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return names
		}
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasSuffix(name, "_fuel.csv") && strings.Contains(name, "_"+shortName+"_") {
			names = append(names, name)
		}
	}
	return names
}

func findFuelLog(t *testing.T, dir, shortName string) string {
	t.Helper()
	names := fuelLogsMatching(t, dir, shortName)
	if len(names) != 1 {
		t.Fatalf("expected exactly one FUEL log for %q, found %v", shortName, names)
	}
	return names[0]
}

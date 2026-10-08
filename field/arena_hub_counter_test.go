package field

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/team841/bioarena/game"
	"github.com/team841/bioarena/hardware"
	"github.com/team841/bioarena/model"
	"github.com/team841/bioarena/plc"
	"github.com/team841/bioarena/websocket"
)

// startedArena is an arena in AUTO with one robot on the field, which is what the scoring
// path needs before it will attribute anything.
func startedArena(t *testing.T) (*Arena, *FakePlc) {
	t.Helper()
	arena := setupTestArena(t)

	fakePlc := new(FakePlc)
	arena.Plc = fakePlc

	assert.Nil(t, arena.Database.CreateTeam(&model.Team{Id: 254}))
	assert.Nil(t, arena.assignTeam(254, "R1"))
	arena.AllianceStations["R1"].DsConn = &DriverStationConnection{TeamId: 254}
	arena.AllianceStations["R1"].DsConn.RobotLinked = true
	for _, station := range []string{"R2", "R3", "B1", "B2", "B3"} {
		arena.AllianceStations[station].Bypass.Store(true)
	}

	assert.Nil(t, arena.StartMatch())
	arena.Update() // consumes StartMatch, which is also what sets MatchStartTime
	arena.MatchStartTime = time.Now().Add(-time.Duration(game.MatchTiming.WarmupDurationSec) * time.Second)
	arena.Update()
	assert.Equal(t, AutoPeriod, arena.MatchState)

	return arena, fakePlc
}

// The two scoring lines are upstream's, so the shift they credit has to be upstream's too.
func TestUpdateHubCountsCreditsTheCurrentShift(t *testing.T) {
	arena, fakePlc := startedArena(t)

	fakePlc.redHubCount = 3
	fakePlc.blueHubCount = 1
	arena.Update()

	assert.Equal(t, 3, arena.RedHub.GetShiftCount(game.ShiftAuto, true))
	assert.Equal(t, 1, arena.BlueHub.GetShiftCount(game.ShiftAuto, true))

	// Counts are cumulative, so only the increment is new FUEL.
	fakePlc.redHubCount = 5
	arena.Update()
	assert.Equal(t, 5, arena.RedHub.GetShiftCount(game.ShiftAuto, true))
	assert.Equal(t, 1, arena.BlueHub.GetShiftCount(game.ShiftAuto, true))
}

// The whole point of the override: FUEL reported by the counter has to reach the Hubs
// through arena.Plc.GetHubCounts, the call upstream reads its PLC with.
func TestUpdateHubCountsReadsThroughTheOverride(t *testing.T) {
	arena, _ := startedArena(t)

	source := &fakeHubCountSource{red: 4, blue: 2}
	arena.Plc = plc.WithHubCounts(arena.Plc, source)

	arena.Update()

	assert.Equal(t, 4, arena.RedHub.GetShiftCount(game.ShiftAuto, true))
	assert.Equal(t, 2, arena.BlueHub.GetShiftCount(game.ShiftAuto, true))
}

// One round's FUEL must not be in the next one's tally. Clearing a match loads a fresh
// one, which is where the tallies go -- and the counter is reset with them, since its
// baseline rides the same Plc.ResetMatch call.
func TestClearingAMatchZeroesTheHubsAndTheCounter(t *testing.T) {
	arena, _ := startedArena(t)
	source := &fakeHubCountSource{red: 6, blue: 2}
	arena.Plc = plc.WithHubCounts(arena.Plc, source)

	arena.Update()
	assert.Equal(t, 6, arena.RedHub.GetShiftCount(game.ShiftAuto, true))
	assert.Equal(t, 2, arena.BlueHub.GetShiftCount(game.ShiftAuto, true))

	assert.Nil(t, arena.AbortMatch())
	assert.Equal(t, PostMatch, arena.MatchState)
	assert.Nil(t, arena.ClearMatch())
	assert.Equal(t, PreMatch, arena.MatchState)

	assert.Equal(t, 0, hubTotalCount(&arena.RedHub))
	assert.Equal(t, 0, hubTotalCount(&arena.BlueHub))
	assert.Greater(t, source.resets, 0, "the counter's baseline rides Plc.ResetMatch")
}

// And starting a match takes a fresh baseline of its own, so anything the counter reported
// between loading the match and running it is not part of it.
func TestStartingAMatchTakesAFreshCounterBaseline(t *testing.T) {
	arena, _ := startedArena(t)
	source := &fakeHubCountSource{}
	arena.Plc = plc.WithHubCounts(arena.Plc, source)

	assert.Nil(t, arena.AbortMatch())
	assert.Nil(t, arena.ClearMatch())
	assert.Equal(t, PreMatch, arena.MatchState)

	// Bypassed rather than re-linked: the fake driver station connection goes stale once
	// Update has recalculated it, and readiness is not what this test is about.
	for _, station := range allStationNames {
		arena.AllianceStations[station].Bypass.Store(true)
	}

	resetsBefore := source.resets
	assert.Nil(t, arena.StartMatch())
	arena.Update()

	assert.Greater(t, source.resets, resetsBefore)
	assert.Equal(t, 0, hubTotalCount(&arena.RedHub))
	assert.Equal(t, 0, hubTotalCount(&arena.BlueHub))
}

// FUEL into a dark HUB is counted but not credited, and the difference is the number a
// practice field exists to show a driver.
func TestHubCountsSeparateCountedFromCredited(t *testing.T) {
	arena, fakePlc := startedArena(t)

	// Red won AUTO, so red's HUB is dark for shifts 1 and 3.
	arena.AutoWinner = hardware.AllianceRed
	arena.MatchState = TeleopPeriod
	arena.MatchStartTime = time.Now().Add(-midShiftOffset(game.Shift1))

	fakePlc.redHubCount = 7
	arena.Update()

	redHub, _ := arena.hubs()
	assert.Equal(t, 7, redHub.GetShiftCount(game.Shift1, false), "the camera saw seven")
	assert.Equal(t, 0, redHub.GetShiftCount(game.Shift1, true), "none of them scored")
	assert.Equal(t, 7, hubTotalCount(redHub))
	assert.Equal(t, 0, hubCreditedCount(redHub))
}

// hubs must read WonAuto from arena.AutoWinner every time, or the lighting and the scoring
// end up disagreeing about which HUB is dark.
func TestHubsFollowTheAutoWinner(t *testing.T) {
	arena := setupTestArena(t)

	// Undecided: neither HUB has won, which is symmetric rather than arbitrary.
	arena.AutoWinner = hardware.AllianceNone
	redHub, blueHub := arena.hubs()
	assert.False(t, redHub.WonAuto)
	assert.False(t, blueHub.WonAuto)

	arena.AutoWinner = hardware.AllianceRed
	redHub, blueHub = arena.hubs()
	assert.True(t, redHub.WonAuto)
	assert.False(t, blueHub.WonAuto)

	arena.AutoWinner = hardware.AllianceBlue
	redHub, blueHub = arena.hubs()
	assert.False(t, redHub.WonAuto)
	assert.True(t, blueHub.WonAuto)
}

// --- The match context published to the counter ---

func TestHubMatchContextDescribesTheMatch(t *testing.T) {
	arena, fakePlc := startedArena(t)
	arena.AutoWinner = hardware.AllianceRed
	fakePlc.redHubCount = 2
	arena.Update()

	context := arena.hubMatchContext(time.Now())
	assert.Equal(t, "AUTO_PERIOD", context.MatchState)
	assert.Equal(t, "AUTO", context.Shift)
	assert.True(t, context.RedActive, "both HUBs are active during AUTO")
	assert.True(t, context.BlueActive)
	assert.Equal(t, 2, context.RedCount)
	assert.Equal(t, 2, context.RedCredited)
	assert.Equal(t, 2, context.RedAuto)
	assert.Equal(t, 0, context.BlueCount)
}

// Outside a shift the counter is told so, rather than being given a stale one.
func TestHubMatchContextReportsNoShiftWhenThereIsNone(t *testing.T) {
	arena := setupTestArena(t)
	arena.MatchState = PreMatch

	context := arena.hubMatchContext(time.Now())
	assert.Equal(t, "PRE_MATCH", context.MatchState)
	assert.Equal(t, "NONE", context.Shift)
	assert.False(t, context.RedActive)
	assert.False(t, context.BlueActive)
}

// The names are wire values in the counter protocol, so every state and shift has to have
// one -- a missing case would reach the counter as "UNKNOWN" or "NONE" and look like a
// fault in its own code.
func TestHubProtocolNamesCoverEveryStateAndShift(t *testing.T) {
	for state, expected := range map[MatchState]string{
		PreMatch:     "PRE_MATCH",
		StartMatch:   "START_MATCH",
		WarmupPeriod: "WARMUP_PERIOD",
		AutoPeriod:   "AUTO_PERIOD",
		PausePeriod:  "PAUSE_PERIOD",
		TeleopPeriod: "TELEOP_PERIOD",
		PostMatch:    "POST_MATCH",
	} {
		assert.Equal(t, expected, hubMatchStateName(state))
	}
	assert.Equal(t, "UNKNOWN", hubMatchStateName(MatchState(99)))

	for shift, expected := range map[game.Shift]string{
		game.ShiftAuto:       "AUTO",
		game.ShiftTransition: "TRANSITION",
		game.Shift1:          "SHIFT1",
		game.Shift2:          "SHIFT2",
		game.Shift3:          "SHIFT3",
		game.Shift4:          "SHIFT4",
		game.ShiftEndgame:    "ENDGAME",
		game.ShiftPostMatch:  "POST_MATCH",
	} {
		assert.Equal(t, expected, hubShiftName(shift))
	}
	assert.Equal(t, "NONE", hubShiftName(game.ShiftCount))
}

// --- The score notifier ---

// Scores do not ride arenaStatus, which goes out every 500ms. Firing on change is what
// keeps the tally inside the display budget, so it has to actually fire on one.
func TestHubScoreNotifierFiresOnAChangeOnly(t *testing.T) {
	arena, fakePlc := startedArena(t)

	// Notify always calls the message producer, with or without listeners, so counting
	// producer calls counts fires -- and does it through the exported API rather than by
	// reaching into the notifier.
	fires := 0
	arena.HubScoreNotifier = websocket.NewNotifier("hubScore", func() any {
		fires++
		return nil
	})

	fires = 0
	fakePlc.redHubCount = 1
	arena.Update()
	assert.Equal(t, 1, fires, "a ball scored is a change")

	fires = 0
	arena.Update()
	arena.Update()
	assert.Equal(t, 0, fires, "an unchanged tally must not wake every kiosk 100x/s")

	fires = 0
	fakePlc.redHubCount = 2
	arena.Update()
	assert.Equal(t, 1, fires)
}

// Going offline is a change the panel shows, so it has to fire too -- otherwise a dead
// camera leaves a green badge until the next ball, which is the one thing that will not
// arrive.
func TestHubScoreNotifierFiresWhenTheFeedChangesState(t *testing.T) {
	arena, _ := startedArena(t)
	arena.applyHubCounterSettings(&model.EventSettings{
		HubCounterAddress: "127.0.0.1",
		HubCounterPort:    0,
	})
	defer arena.applyHubCounterSettings(&model.EventSettings{})

	fires := 0
	arena.HubScoreNotifier = websocket.NewNotifier("hubScore", func() any {
		fires++
		return nil
	})

	// First tick records OFFLINE, since nothing has been heard from the counter.
	arena.Update()
	fires = 0
	arena.Update()
	assert.Equal(t, 0, fires, "still offline is not a change")
}

func TestHubScoreMessageCarriesBothHubs(t *testing.T) {
	arena, fakePlc := startedArena(t)
	fakePlc.redHubCount = 4
	fakePlc.blueHubCount = 1
	arena.Update()

	message := arena.generateHubScoreMessage()
	encoded, err := json.Marshal(message)
	assert.Nil(t, err)

	var decoded struct {
		Counter struct {
			Status    string
			LatencyMs int
		}
		Red  hubScoreView
		Blue hubScoreView
	}
	assert.Nil(t, json.Unmarshal(encoded, &decoded))

	assert.Equal(t, HubCounterNotConfigured, decoded.Counter.Status)
	assert.Equal(t, 4, decoded.Red.Counted)
	assert.Equal(t, 4, decoded.Red.Credited)
	assert.Equal(t, 4, decoded.Red.Auto)
	assert.Equal(t, 1, decoded.Blue.Counted)
	assert.Len(t, decoded.Red.ByShift, int(game.ShiftCount))
	assert.Equal(t, 4, decoded.Red.ByShift[game.ShiftAuto])
}

// FUEL credited to an alliance with nobody on it is the wrong-side mistake: a driver
// station plugged into the other alliance, or a camera with the hubs swapped.
func TestHubScoreWarnsWhenAnAllianceHasNoTeam(t *testing.T) {
	arena, fakePlc := startedArena(t)

	// startedArena registers 254 on R1 and bypasses the rest, so blue has no team.
	fakePlc.redHubCount = 2
	fakePlc.blueHubCount = 3
	arena.Update()

	message := arena.generateHubScoreMessage()
	encoded, _ := json.Marshal(message)
	var decoded struct {
		Red  hubScoreView
		Blue hubScoreView
	}
	assert.Nil(t, json.Unmarshal(encoded, &decoded))

	assert.False(t, decoded.Red.NoTeam, "red has team 254 registered")
	assert.True(t, decoded.Blue.NoTeam, "blue scored with nobody on it")
}

// The warning has to survive the HUB being dark. A driver on the wrong side shooting
// during a shift their HUB is inactive for scores nothing, so a warning keyed on the score
// would go quiet for half of teleop -- exactly while the operator is staring at a tally
// that is not moving and has no idea why.
func TestHubScoreWarnsEvenWhenTheWrongSideScoresNothing(t *testing.T) {
	arena, fakePlc := startedArena(t)

	// Blue won AUTO, so blue's HUB is dark for shifts 1 and 3, and blue has no team.
	arena.AutoWinner = hardware.AllianceBlue
	arena.MatchState = TeleopPeriod
	arena.MatchStartTime = time.Now().Add(-midShiftOffset(game.Shift1))

	fakePlc.blueHubCount = 5
	arena.Update()

	_, blueHub := arena.hubs()
	assert.Equal(t, 5, hubTotalCount(blueHub))
	assert.Equal(t, 0, hubCreditedCount(blueHub), "a dark HUB credits nothing")

	message := arena.generateHubScoreMessage()
	encoded, _ := json.Marshal(message)
	var decoded struct{ Blue hubScoreView }
	assert.Nil(t, json.Unmarshal(encoded, &decoded))
	assert.True(t, decoded.Blue.NoTeam, "five balls went somewhere nobody is registered")
}

// An alliance that has seen no FUEL at all is not a wrong-side mistake, just the empty
// half of a 1v0. Warning there would fire on every practice match from the first tick.
func TestHubScoreDoesNotWarnWithoutAnyFuel(t *testing.T) {
	arena, _ := startedArena(t)
	arena.Update()

	message := arena.generateHubScoreMessage()
	encoded, _ := json.Marshal(message)
	var decoded struct {
		Blue hubScoreView
	}
	assert.Nil(t, json.Unmarshal(encoded, &decoded))
	assert.False(t, decoded.Blue.NoTeam)
}

// --- Counted mode ---

// scoreInAuto puts FUEL into each HUB's AUTO shift, which is what the Counted decision
// reads.
func scoreInAuto(t *testing.T, arena *Arena, fakePlc *FakePlc, red, blue int) {
	t.Helper()
	fakePlc.redHubCount, fakePlc.blueHubCount = red, blue
	arena.Update()
	redHub, blueHub := arena.hubs()
	assert.Equal(t, red, redHub.GetShiftCount(game.ShiftAuto, true))
	assert.Equal(t, blue, blueHub.GetShiftCount(game.ShiftAuto, true))
}

func TestCountedAutoWinnerFollowsTheFuel(t *testing.T) {
	for _, c := range []struct {
		name      string
		red, blue int
		expected  hardware.Alliance
	}{
		{"red scored more", 7, 2, hardware.AllianceRed},
		{"blue scored more", 1, 9, hardware.AllianceBlue},
		// A half field: one HUB counting, the other reporting nothing all match. Any FUEL
		// in AUTO takes the advantage.
		{"only red has a hub", 1, 0, hardware.AllianceRed},
		{"only blue has a hub", 0, 1, hardware.AllianceBlue},
	} {
		arena, fakePlc := startedArena(t)
		arena.AutoWinnerMode = AutoWinnerCounted
		scoreInAuto(t, arena, fakePlc, c.red, c.blue)

		arena.assignAutoWinner()

		assert.Equal(t, c.expected, arena.AutoWinner, c.name)
	}
}

// A tie is broken by selecting an alliance, as a real field does -- including the tie at
// zero, which is every match where nobody scores in AUTO. Never AllianceNone.
func TestCountedAutoWinnerBreaksATieBySelecting(t *testing.T) {
	// One arena, decided repeatedly. SetupTestArena seeds math/rand to a fixed value for
	// reproducibility, so building a fresh arena per iteration would reseed it and make
	// every "coin flip" land the same way.
	arena, fakePlc := startedArena(t)
	arena.AutoWinnerMode = AutoWinnerCounted
	scoreInAuto(t, arena, fakePlc, 3, 3)

	seen := map[hardware.Alliance]bool{}
	for i := 0; i < 60; i++ {
		arena.assignAutoWinner()
		assert.NotEqual(t, hardware.AllianceNone, arena.AutoWinner)
		seen[arena.AutoWinner] = true
	}
	assert.True(t, seen[hardware.AllianceRed] && seen[hardware.AllianceBlue],
		"a tie must be able to go either way")
}

// The whole point of Counted mode: the game data the driver station receives has to agree
// with what was actually scored.
func TestCountedAutoWinnerDrivesGameData(t *testing.T) {
	arena, fakePlc := startedArena(t)
	arena.AutoWinnerMode = AutoWinnerCounted
	scoreInAuto(t, arena, fakePlc, 6, 1)

	// Run the clock to the teleop transition, where the result is settled and released.
	arena.MatchStartTime = time.Now().Add(
		-time.Duration(
			game.MatchTiming.WarmupDurationSec+game.MatchTiming.AutoDurationSec,
		) * time.Second,
	)
	arena.Update()
	assert.Equal(t, PausePeriod, arena.MatchState)
	assert.Equal(t, "", arena.GameData, "nothing is released before the transition")

	arena.MatchStartTime = time.Now().Add(
		-time.Duration(
			game.MatchTiming.WarmupDurationSec+game.MatchTiming.AutoDurationSec+
				game.MatchTiming.PauseDurationSec,
		) * time.Second,
	)
	arena.Update()

	assert.Equal(t, TeleopPeriod, arena.MatchState)
	assert.Equal(t, hardware.AllianceRed, arena.AutoWinner)
	assert.Equal(t, "R", arena.GameData)
}

// FUEL that arrives during the pause still counts for AUTO -- upstream's grace period is
// the whole pause, and it is what makes a ball shot at the buzzer count.
func TestCountedAutoWinnerIncludesFuelFromTheGracePeriod(t *testing.T) {
	arena, fakePlc := startedArena(t)
	arena.AutoWinnerMode = AutoWinnerCounted
	scoreInAuto(t, arena, fakePlc, 1, 2) // blue ahead at the buzzer

	// Into the pause, inside the scoring grace.
	arena.MatchStartTime = time.Now().Add(
		-time.Duration(game.MatchTiming.WarmupDurationSec+game.MatchTiming.AutoDurationSec)*time.Second -
			500*time.Millisecond,
	)
	fakePlc.redHubCount = 4 // three more red, still credited to AUTO
	arena.Update()

	redHub, _ := arena.hubs()
	assert.Equal(t, 4, redHub.GetShiftCount(game.ShiftAuto, true))

	arena.assignAutoWinner()
	assert.Equal(t, hardware.AllianceRed, arena.AutoWinner)
}

// --- Counted-mode readiness ---

func TestCountedModeBlocksStartWithoutACounter(t *testing.T) {
	arena := setupTestArena(t)
	arena.Plc = new(FakePlc)
	for _, station := range allStationNames {
		arena.AllianceStations[station].Bypass.Store(true)
	}

	arena.AutoWinnerMode = AutoWinnerRandom
	assert.Nil(t, arena.checkCanStartMatch(), "the other modes do not need a counter")

	arena.AutoWinnerMode = AutoWinnerCounted
	err := arena.checkCanStartMatch()
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "no Hub FUEL counter is configured")
}

// A counter that is configured but not talking is worse than none: the match would run to
// completion looking correct while the AUTO advantage was decided by a coin flip.
func TestCountedModeBlocksStartWhileTheCounterIsOffline(t *testing.T) {
	arena := setupTestArena(t)
	arena.Plc = new(FakePlc)
	for _, station := range allStationNames {
		arena.AllianceStations[station].Bypass.Store(true)
	}
	arena.applyHubCounterSettings(&model.EventSettings{
		HubCounterAddress: "127.0.0.1",
		HubCounterPort:    0,
	})
	defer arena.applyHubCounterSettings(&model.EventSettings{})

	arena.AutoWinnerMode = AutoWinnerCounted
	err := arena.checkCanStartMatch()
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "OFFLINE")

	// Display-only in the other modes, so a dead camera must not stop practice.
	arena.AutoWinnerMode = AutoWinnerRandom
	assert.Nil(t, arena.checkCanStartMatch())
}

// --- Persistence ---

func TestAutoWinnerModePersists(t *testing.T) {
	arena := setupTestArena(t)

	assert.Equal(t, AutoWinnerRandom, arena.AutoWinnerMode, "random until told otherwise")

	assert.Nil(t, arena.SetAutoWinnerMode(AutoWinnerCounted))
	assert.Equal(t, "counted", arena.EventSettings.AutoWinnerMode)

	// Reloading is what a restart does.
	assert.Nil(t, arena.LoadSettings())
	assert.Equal(t, AutoWinnerCounted, arena.AutoWinnerMode)

	assert.Nil(t, arena.SetAutoWinnerMode(AutoWinnerForceBlue))
	assert.Nil(t, arena.LoadSettings())
	assert.Equal(t, AutoWinnerForceBlue, arena.AutoWinnerMode)
}

// A settings record with a value nothing can parse must not leave the field unable to
// start a match.
func TestAutoWinnerModeFallsBackWhenUnparseable(t *testing.T) {
	arena := setupTestArena(t)

	arena.EventSettings.AutoWinnerMode = "purple"
	assert.Nil(t, arena.Database.UpdateEventSettings(arena.EventSettings))
	assert.Nil(t, arena.LoadSettings())

	assert.Equal(t, AutoWinnerRandom, arena.AutoWinnerMode)
}

// --- Settings ---

func TestApplyHubCounterSettingsStartsAndStopsTheReceiver(t *testing.T) {
	arena := setupTestArena(t)
	arena.Plc = new(FakePlc)

	assert.Nil(t, arena.hubCounter)
	assert.Equal(t, HubCounterNotConfigured, arena.HubCounterStatus())

	settings := &model.EventSettings{HubCounterAddress: "127.0.0.1", HubCounterPort: 0}
	arena.applyHubCounterSettings(settings)
	assert.NotNil(t, arena.hubCounter)
	// Port 0 falls back to the default rather than being taken as "no counter", which is
	// the address's job to say.
	assert.Equal(t, hardware.DefaultHubCounterPort, arena.hubCounter.Port())
	assert.Equal(t, hardware.HubCounterOffline, arena.HubCounterStatus())

	// The receiver has to reach arena.Plc, or the scoring path never sees it.
	red, blue := arena.Plc.GetHubCounts()
	assert.Equal(t, 0, red)
	assert.Equal(t, 0, blue)

	// Clearing the address closes the receiver and unwraps the PLC, so a field that
	// removes its counter is left with the PLC it started with.
	arena.applyHubCounterSettings(&model.EventSettings{})
	assert.Nil(t, arena.hubCounter)
	assert.Equal(t, HubCounterNotConfigured, arena.HubCounterStatus())
}

// A counter that will not start must not stop the arena from loading: scoring is not a
// safety function, and the operator sees NOT CONFIGURED either way.
func TestApplyHubCounterSettingsSurvivesABadAddress(t *testing.T) {
	arena := setupTestArena(t)
	arena.Plc = new(FakePlc)

	arena.applyHubCounterSettings(&model.EventSettings{
		HubCounterAddress: "not-an-address",
		HubCounterPort:    hardware.DefaultHubCounterPort,
	})

	assert.Nil(t, arena.hubCounter)
	assert.Equal(t, HubCounterNotConfigured, arena.HubCounterStatus())
	red, blue := arena.Plc.GetHubCounts()
	assert.Equal(t, 0, red)
	assert.Equal(t, 0, blue)
}

// Reconfiguring must not leave the old receiver holding the UDP port, or the second save
// silently keeps the first save's address.
func TestApplyHubCounterSettingsRebindsThePort(t *testing.T) {
	arena := setupTestArena(t)
	arena.Plc = new(FakePlc)

	settings := &model.EventSettings{HubCounterAddress: "127.0.0.1", HubCounterPort: 8412}
	arena.applyHubCounterSettings(settings)
	assert.NotNil(t, arena.hubCounter)
	assert.Equal(t, 8412, arena.hubCounter.Port())

	arena.applyHubCounterSettings(settings)
	assert.NotNil(t, arena.hubCounter, "the same port has to be bindable again")
	assert.Equal(t, 8412, arena.hubCounter.Port())

	arena.applyHubCounterSettings(&model.EventSettings{})
}

// fakeHubCountSource stands in for the receiver where a test only needs numbers.
type fakeHubCountSource struct {
	red, blue int
	resets    int
}

func (source *fakeHubCountSource) GetHubCounts() (int, int) {
	return source.red, source.blue
}

func (source *fakeHubCountSource) ResetMatch() {
	source.resets++
	source.red, source.blue = 0, 0
}

// midShiftOffset is how far into the match the middle of the given teleop shift is, so a
// test can put the clock somewhere unambiguous inside it.
//
// The middle rather than the start, because the previous shift's scoring grace period
// reaches into the first three seconds of this one: FUEL arriving there still belongs to
// the shift it was shot in, which is the whole point of the grace, but it makes the start
// of a shift the wrong place to ask which shift it is.
func midShiftOffset(shift game.Shift) time.Duration {
	offset := time.Duration(
		game.MatchTiming.WarmupDurationSec+game.MatchTiming.AutoDurationSec+
			game.MatchTiming.PauseDurationSec+game.MatchTiming.TransitionShiftDurationSec,
	) * time.Second
	for s := game.Shift1; s < shift; s++ {
		offset += time.Duration(game.MatchTiming.ShiftDurationSec) * time.Second
	}
	return offset + time.Duration(game.MatchTiming.ShiftDurationSec/2)*time.Second
}

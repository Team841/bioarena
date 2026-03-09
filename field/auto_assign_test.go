// Copyright 2026 Team 254. All Rights Reserved.
//
// Tests for autoAssignTeam in arena.go.

package field

import (
	"fmt"
	"github.com/Team254/cheesy-arena/model"
	"github.com/stretchr/testify/assert"
	"testing"
)

// mockStationDetector satisfies the stationDetector interface for testing switch-based detection.
type mockStationDetector struct {
	station string
	err     error
}

func (m *mockStationDetector) GetStationForTeamId(_ int) (string, error) {
	return m.station, m.err
}

func TestAutoAssignTeamFallbackToR1(t *testing.T) {
	arena := setupTestArena(t)
	// Default arena has a test match loaded (ShouldAllowSubstitution = true) and PreMatch state.
	// Switch address is "" so GetStationForTeamId returns ""; falls back to R1.
	station := arena.autoAssignTeam(254)
	assert.Equal(t, "R1", station)
	assert.Equal(t, 254, arena.CurrentMatch.Red1)
	assert.NotNil(t, arena.AllianceStations["R1"].Team)
	assert.Equal(t, 254, arena.AllianceStations["R1"].Team.Id)
}

func TestAutoAssignTeamSequentialFill(t *testing.T) {
	arena := setupTestArena(t)
	// First team goes to R1, second to R2.
	assert.Equal(t, "R1", arena.autoAssignTeam(111))
	assert.Equal(t, "R2", arena.autoAssignTeam(222))
	assert.Equal(t, "R3", arena.autoAssignTeam(333))
	assert.Equal(t, "B1", arena.autoAssignTeam(444))
	assert.Equal(t, "B2", arena.autoAssignTeam(555))
	assert.Equal(t, "B3", arena.autoAssignTeam(666))
}

func TestAutoAssignTeamAllStationsOccupied(t *testing.T) {
	arena := setupTestArena(t)
	arena.autoAssignTeam(111)
	arena.autoAssignTeam(222)
	arena.autoAssignTeam(333)
	arena.autoAssignTeam(444)
	arena.autoAssignTeam(555)
	arena.autoAssignTeam(666)
	// All stations full; should return "".
	station := arena.autoAssignTeam(777)
	assert.Equal(t, "", station)
}

func TestAutoAssignTeamExistingDbRecordNotOverwritten(t *testing.T) {
	arena := setupTestArena(t)
	// Pre-create team with a custom WPA key.
	existing := &model.Team{Id: 254, WpaKey: "mykey1234"}
	assert.Nil(t, arena.Database.CreateTeam(existing))

	arena.autoAssignTeam(254)

	// Verify WPA key was not overwritten.
	team, err := arena.Database.GetTeamById(254)
	assert.Nil(t, err)
	assert.Equal(t, "mykey1234", team.WpaKey)
}

func TestAutoAssignTeamCreatesTeamWithPredictableWpaKey(t *testing.T) {
	arena := setupTestArena(t)
	arena.autoAssignTeam(254)

	team, err := arena.Database.GetTeamById(254)
	assert.Nil(t, err)
	assert.NotNil(t, team)
	assert.Equal(t, "00000254", team.WpaKey)
}

func TestAutoAssignTeamNotPreMatch(t *testing.T) {
	arena := setupTestArena(t)
	arena.MatchState = AutoPeriod
	station := arena.autoAssignTeam(254)
	assert.Equal(t, "", station)
	assert.Nil(t, arena.AllianceStations["R1"].Team)
}

func TestAutoAssignTeamQualificationMatch(t *testing.T) {
	arena := setupTestArena(t)
	qualMatch := model.Match{Type: model.Qualification, ShortName: "Q1", LongName: "Qualification 1"}
	assert.Nil(t, arena.Database.CreateMatch(&qualMatch))
	assert.Nil(t, arena.LoadMatch(&qualMatch))

	station := arena.autoAssignTeam(254)
	assert.Equal(t, "", station)
	assert.Nil(t, arena.AllianceStations["R1"].Team)
}

func TestAutoAssignTeamMatchFieldsAllUpdated(t *testing.T) {
	arena := setupTestArena(t)
	arena.autoAssignTeam(111)
	arena.autoAssignTeam(222)
	arena.autoAssignTeam(333)
	arena.autoAssignTeam(444)
	arena.autoAssignTeam(555)
	arena.autoAssignTeam(666)
	assert.Equal(t, 111, arena.CurrentMatch.Red1)
	assert.Equal(t, 222, arena.CurrentMatch.Red2)
	assert.Equal(t, 333, arena.CurrentMatch.Red3)
	assert.Equal(t, 444, arena.CurrentMatch.Blue1)
	assert.Equal(t, 555, arena.CurrentMatch.Blue2)
	assert.Equal(t, 666, arena.CurrentMatch.Blue3)
}

func TestAutoAssignTeamWpaKeyEdgeCases(t *testing.T) {
	arena := setupTestArena(t)

	arena.autoAssignTeam(1)
	team1, err := arena.Database.GetTeamById(1)
	assert.Nil(t, err)
	assert.Equal(t, "00000001", team1.WpaKey)

	arena.autoAssignTeam(9999)
	team9999, err := arena.Database.GetTeamById(9999)
	assert.Nil(t, err)
	assert.Equal(t, "00009999", team9999.WpaKey)
}

func TestAutoAssignTeamPracticeMatchPersistsToDb(t *testing.T) {
	arena := setupTestArena(t)
	practiceMatch := model.Match{Type: model.Practice, ShortName: "P1", LongName: "Practice 1"}
	assert.Nil(t, arena.Database.CreateMatch(&practiceMatch))
	assert.Nil(t, arena.LoadMatch(&practiceMatch))

	arena.autoAssignTeam(254)

	saved, err := arena.Database.GetMatchById(practiceMatch.Id)
	assert.Nil(t, err)
	assert.Equal(t, 254, saved.Red1)
}

func TestAutoAssignTeamAdditionalStateGuards(t *testing.T) {
	for _, state := range []MatchState{WarmupPeriod, TeleopPeriod, PostMatch} {
		arena := setupTestArena(t)
		arena.MatchState = state
		station := arena.autoAssignTeam(254)
		assert.Equal(t, "", station, "expected no assignment in state %v", state)
		assert.Nil(t, arena.AllianceStations["R1"].Team, "expected no team in R1 in state %v", state)
	}
}

func TestAutoAssignTeamSwitchDetectsStation(t *testing.T) {
	arena := setupTestArena(t)
	arena.stationDetectorOverride = &mockStationDetector{station: "B3"}

	station := arena.autoAssignTeam(254)
	assert.Equal(t, "B3", station)
	assert.NotNil(t, arena.AllianceStations["B3"].Team)
	assert.Equal(t, 254, arena.AllianceStations["B3"].Team.Id)
	assert.Nil(t, arena.AllianceStations["R1"].Team)
}

func TestAutoAssignTeamSwitchStationOccupied(t *testing.T) {
	arena := setupTestArena(t)
	arena.autoAssignTeam(111) // occupies R1
	arena.stationDetectorOverride = &mockStationDetector{station: "R1"}

	station := arena.autoAssignTeam(222)
	assert.Equal(t, "R2", station)
	assert.NotNil(t, arena.AllianceStations["R2"].Team)
	assert.Equal(t, 222, arena.AllianceStations["R2"].Team.Id)
}

func TestAutoAssignTeamSwitchError(t *testing.T) {
	arena := setupTestArena(t)
	arena.stationDetectorOverride = &mockStationDetector{err: fmt.Errorf("telnet timeout")}

	station := arena.autoAssignTeam(254)
	assert.Equal(t, "R1", station)
	assert.NotNil(t, arena.AllianceStations["R1"].Team)
	assert.Equal(t, 254, arena.AllianceStations["R1"].Team.Id)
}

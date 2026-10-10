// Copyright 2014 Team 254. All Rights Reserved.
// Portions Copyright Team 841. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)

package web

import (
	"bytes"
	"github.com/team841/bioarena/model"
	"github.com/stretchr/testify/assert"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSetupSettings(t *testing.T) {
	web := setupTestWeb(t)

	// Check the default setting values.
	recorder := web.getHttpResponse("/setup/settings")
	assert.Equal(t, 200, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "Untitled Event")

	// Change the name and check the response.
	recorder = web.postHttpResponse("/setup/settings", "name=Chezy Champs")
	assert.Equal(t, 303, recorder.Code)
	recorder = web.getHttpResponse("/setup/settings")
	assert.Contains(t, recorder.Body.String(), "Chezy Champs")
}

func TestSetupSettingsClearDb(t *testing.T) {
	createData := func(web *Web) {
		assert.Nil(t, web.arena.Database.CreateTeam(&model.Team{Id: 254}))
		assert.Nil(t, web.arena.Database.CreateMatch(&model.Match{Type: model.Practice}))
		assert.Nil(t, web.arena.Database.CreateMatch(&model.Match{Type: model.Qualification}))
		assert.Nil(t, web.arena.Database.CreateMatch(&model.Match{Type: model.Playoff}))
		assert.Nil(t, web.arena.Database.CreateMatchResult(&model.MatchResult{MatchId: 1, PlayNumber: 1}))
		assert.Nil(t, web.arena.Database.CreateMatchResult(&model.MatchResult{MatchId: 1, PlayNumber: 2}))
		assert.Nil(t, web.arena.Database.CreateMatchResult(&model.MatchResult{MatchId: 2, PlayNumber: 1}))
		assert.Nil(t, web.arena.Database.CreateMatchResult(&model.MatchResult{MatchId: 3, PlayNumber: 1}))
		assert.Nil(t, web.arena.Database.CreateAlliance(&model.Alliance{Id: 1}))
	}

	// Test clearing practice data.
	web := setupTestWeb(t)
	createData(web)
	recorder := web.postHttpResponse("/setup/db/clear/practice", "")
	assert.Equal(t, 303, recorder.Code)
	teams, _ := web.arena.Database.GetAllTeams()
	assert.NotEmpty(t, teams)
	matches, _ := web.arena.Database.GetMatchesByType(model.Practice, true)
	assert.Empty(t, matches)
	matchResult, _ := web.arena.Database.GetMatchResultForMatch(1)
	assert.Nil(t, matchResult)
	matches, _ = web.arena.Database.GetMatchesByType(model.Qualification, true)
	assert.NotEmpty(t, matches)
	matchResult, _ = web.arena.Database.GetMatchResultForMatch(2)
	assert.NotNil(t, matchResult)
	matches, _ = web.arena.Database.GetMatchesByType(model.Playoff, true)
	assert.NotEmpty(t, matches)
	matchResult, _ = web.arena.Database.GetMatchResultForMatch(3)
	assert.NotNil(t, matchResult)
	alliances, _ := web.arena.Database.GetAllAlliances()
	assert.NotEmpty(t, alliances)

	// Test clearing qualification data.
	web = setupTestWeb(t)
	createData(web)
	recorder = web.postHttpResponse("/setup/db/clear/qualification", "")
	assert.Equal(t, 303, recorder.Code)
	teams, _ = web.arena.Database.GetAllTeams()
	assert.NotEmpty(t, teams)
	matches, _ = web.arena.Database.GetMatchesByType(model.Practice, true)
	assert.NotEmpty(t, matches)
	matchResult, _ = web.arena.Database.GetMatchResultForMatch(1)
	assert.NotNil(t, matchResult)
	matches, _ = web.arena.Database.GetMatchesByType(model.Qualification, true)
	assert.Empty(t, matches)
	matchResult, _ = web.arena.Database.GetMatchResultForMatch(2)
	assert.Nil(t, matchResult)
	matches, _ = web.arena.Database.GetMatchesByType(model.Playoff, true)
	assert.NotEmpty(t, matches)
	matchResult, _ = web.arena.Database.GetMatchResultForMatch(3)
	assert.NotNil(t, matchResult)
	alliances, _ = web.arena.Database.GetAllAlliances()
	assert.NotEmpty(t, alliances)

	// Test clearing playoff data.
	web = setupTestWeb(t)
	createData(web)
	recorder = web.postHttpResponse("/setup/db/clear/playoff", "")
	assert.Equal(t, 303, recorder.Code)
	teams, _ = web.arena.Database.GetAllTeams()
	assert.NotEmpty(t, teams)
	matches, _ = web.arena.Database.GetMatchesByType(model.Practice, true)
	assert.NotEmpty(t, matches)
	matchResult, _ = web.arena.Database.GetMatchResultForMatch(1)
	assert.NotNil(t, matchResult)
	matches, _ = web.arena.Database.GetMatchesByType(model.Qualification, true)
	assert.NotEmpty(t, matches)
	matchResult, _ = web.arena.Database.GetMatchResultForMatch(2)
	assert.NotNil(t, matchResult)
	matches, _ = web.arena.Database.GetMatchesByType(model.Playoff, true)
	assert.Empty(t, matches)
	matchResult, _ = web.arena.Database.GetMatchResultForMatch(3)
	assert.Nil(t, matchResult)
	alliances, _ = web.arena.Database.GetAllAlliances()
	assert.Empty(t, alliances)

	// Test with invalid match types.
	recorder = web.postHttpResponse("/setup/db/clear/all", "")
	assert.Equal(t, 200, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "Invalid tournament stage to clear")
	recorder = web.postHttpResponse("/setup/db/clear/test", "")
	assert.Equal(t, 200, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "Invalid tournament stage to clear")
}

func TestSetupSettingsBackupRestoreDb(t *testing.T) {
	web := setupTestWeb(t)

	// Modify a parameter so that we know when the database has been restored.
	web.arena.EventSettings.Name = "Chezy Champs"
	assert.Nil(t, web.arena.Database.UpdateEventSettings(web.arena.EventSettings))

	// Back up the database.
	recorder := web.getHttpResponse("/setup/db/save")
	assert.Equal(t, 200, recorder.Code)
	assert.Equal(t, "application/octet-stream", recorder.HeaderMap["Content-Type"][0])
	backupBody := recorder.Body

	// Wipe the database to reset the defaults.
	web = setupTestWeb(t)
	assert.NotEqual(t, "Chezy Champs", web.arena.EventSettings.Name)

	// Check restoring with a missing file.
	recorder = web.postHttpResponse("/setup/db/restore", "")
	assert.Contains(t, recorder.Body.String(), "No database backup file was specified")
	assert.NotEqual(t, "Chezy Champs", web.arena.EventSettings.Name)

	// Check restoring with a corrupt file.
	recorder = web.postFileHttpResponse("/setup/db/restore", "databaseFile", bytes.NewBufferString("invalid"))
	assert.Contains(t, recorder.Body.String(), "Could not read uploaded database backup file")
	assert.NotEqual(t, "Chezy Champs", web.arena.EventSettings.Name)

	// Check restoring with the backup retrieved before.
	recorder = web.postFileHttpResponse("/setup/db/restore", "databaseFile", backupBody)
	assert.Equal(t, "Chezy Champs", web.arena.EventSettings.Name)
}

func TestSetupSettingsFieldTabRemovedFields(t *testing.T) {
	web := setupTestWeb(t)
	recorder := web.getHttpResponse("/setup/settings")
	assert.Equal(t, 200, recorder.Code)
	body := recorder.Body.String()
	assert.NotContains(t, body, "apPassword")

	// The driver station ports are fixed by the reference field wiring, not configured.
	assert.NotContains(t, body, "switchDSPortInterfaces")
}

func TestSetupSettingsOmittedFieldBehavior(t *testing.T) {
	web := setupTestWeb(t)

	web.arena.EventSettings.ApPassword = "secret"
	assert.Nil(t, web.arena.Database.UpdateEventSettings(web.arena.EventSettings))

	// POST without apPassword.
	recorder := web.postHttpResponse("/setup/settings", "name=Test+Event")
	assert.Equal(t, 303, recorder.Code)

	// AP password should be cleared to empty string (intentional default).
	assert.Equal(t, "", web.arena.EventSettings.ApPassword)
}

// The relay settings round-trip through the form and survive into the database, so they
// hold across a power cycle. On this platform the pins cannot be opened, which must not stop
// the save: the output is logged as unavailable and the field carries on.
func TestSetupSettingsHubRelays(t *testing.T) {
	web := setupTestWeb(t)

	recorder := web.getHttpResponse("/setup/settings")
	assert.Equal(t, 200, recorder.Code)
	assert.Regexp(t, `name="hubRelayRedPin"\s+value="22"`, recorder.Body.String(), "default pin offered")

	recorder = web.postHttpResponse("/setup/settings",
		"name=Test&hubLightsOutput=relays&hubRelayRedPin=17&hubRelayBluePin=27&hubRelayActiveLow=on")
	assert.Equal(t, 303, recorder.Code)

	saved, err := web.arena.Database.GetEventSettings()
	assert.Nil(t, err)
	assert.True(t, saved.HubRelays)
	assert.Equal(t, 17, saved.HubRelayRedPin)
	assert.Equal(t, 27, saved.HubRelayBluePin)
	assert.True(t, saved.HubRelayActiveLow)

	recorder = web.getHttpResponse("/setup/settings")
	assert.Contains(t, recorder.Body.String(), `<option value="relays" selected>`)

	// Back to DMX; the relay pins are kept for next time.
	recorder = web.postHttpResponse("/setup/settings",
		"name=Test&hubLightsOutput=dmx&hubLedsAddress=10.0.100.100&hubRelayRedPin=17&hubRelayBluePin=27")
	assert.Equal(t, 303, recorder.Code)
	saved, _ = web.arena.Database.GetEventSettings()
	assert.False(t, saved.HubRelays)
	assert.Equal(t, "10.0.100.100", saved.HubLedsAddress)
	assert.Equal(t, 17, saved.HubRelayRedPin)
}

// A relay pin the controller cannot open is refused on the form rather than failing at the
// kernel, where it would leave the lamps dark with only a journal line to explain.
func TestSetupSettingsRejectsBadRelayPins(t *testing.T) {
	for _, c := range []struct {
		form    string
		message string
	}{
		{"hubRelayRedPin=22&hubRelayBluePin=22", "cannot share GPIO pin 22"},
		{"hubRelayRedPin=0&hubRelayBluePin=23", "Red relay GPIO pin must be"},
		{"hubRelayRedPin=22&hubRelayBluePin=40", "Blue relay GPIO pin must be"},
		{"fieldEStopPin=24&hubRelayRedPin=24&hubRelayBluePin=23", "already the field e-stop"},
	} {
		web := setupTestWeb(t)
		recorder := web.postHttpResponse("/setup/settings", "name=Test&hubLightsOutput=relays&"+c.form)
		assert.Equal(t, 200, recorder.Code, c.form)
		assert.Contains(t, recorder.Body.String(), c.message, c.form)
		saved, _ := web.arena.Database.GetEventSettings()
		assert.False(t, saved.HubRelays, "nothing persisted: %s", c.form)
	}

	// Only checked when the relays are the output: DMX never opens the pins.
	web := setupTestWeb(t)
	recorder := web.postHttpResponse("/setup/settings", "name=Test&hubLightsOutput=dmx&hubRelayRedPin=22&hubRelayBluePin=22")
	assert.Equal(t, 303, recorder.Code)
}

// The panel field e-stop ticks are off until set, and stick once they are: a panel built
// without the button must never have it counted by default.
func TestSetupSettingsPanelFieldEStop(t *testing.T) {
	web := setupTestWeb(t)

	recorder := web.getHttpResponse("/setup/settings")
	assert.Contains(t, recorder.Body.String(), `name="blueEStopPanelFieldEStop"`)
	assert.False(t, web.arena.EventSettings.RedEStopPanelFieldEStop)
	assert.False(t, web.arena.EventSettings.BlueEStopPanelFieldEStop)

	recorder = web.postHttpResponse("/setup/settings", "name=Test&blueEStopPanelFieldEStop=on")
	assert.Equal(t, 303, recorder.Code)
	saved, err := web.arena.Database.GetEventSettings()
	assert.Nil(t, err)
	assert.False(t, saved.RedEStopPanelFieldEStop)
	assert.True(t, saved.BlueEStopPanelFieldEStop)

	recorder = web.getHttpResponse("/setup/settings")
	assert.Regexp(t, `name="blueEStopPanelFieldEStop"\s+checked`, recorder.Body.String())
}

func (web *Web) postFileHttpResponse(path string, paramName string, file *bytes.Buffer) *httptest.ResponseRecorder {
	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile(paramName, "file.ext")
	io.Copy(part, file)
	writer.Close()
	recorder := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", path, body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	web.newHandler().ServeHTTP(recorder, req)
	return recorder
}


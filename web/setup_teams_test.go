// Copyright 2014 Team 254. All Rights Reserved.

package web

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Team254/cheesy-arena/model"
	"github.com/stretchr/testify/assert"
)

func TestTeamGetHandlerSuccess(t *testing.T) {
	web := setupTestWeb(t)
	assert.Nil(t, web.arena.Database.CreateTeam(&model.Team{Id: 254, Name: "The Cheesy Poofs", WpaKey: "testkey"}))

	recorder := web.getHttpResponse("/setup/teams/254")
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"))

	var result struct {
		Id     int    `json:"id"`
		Name   string `json:"name"`
		WpaKey string `json:"wpaKey"`
	}
	assert.Nil(t, json.Unmarshal(recorder.Body.Bytes(), &result))
	assert.Equal(t, 254, result.Id)
	assert.Equal(t, "The Cheesy Poofs", result.Name)
	assert.Equal(t, "testkey", result.WpaKey)
}

func TestTeamGetHandlerNotFound(t *testing.T) {
	web := setupTestWeb(t)

	recorder := web.getHttpResponse("/setup/teams/9999")
	assert.Equal(t, http.StatusNotFound, recorder.Code)
}

func TestTeamGetHandlerNoWpaKey(t *testing.T) {
	web := setupTestWeb(t)
	assert.Nil(t, web.arena.Database.CreateTeam(&model.Team{Id: 100, Name: "No Key Team", WpaKey: ""}))

	recorder := web.getHttpResponse("/setup/teams/100")
	assert.Equal(t, http.StatusOK, recorder.Code)

	var result struct {
		Id     int    `json:"id"`
		Name   string `json:"name"`
		WpaKey string `json:"wpaKey"`
	}
	assert.Nil(t, json.Unmarshal(recorder.Body.Bytes(), &result))
	assert.Equal(t, 100, result.Id)
	assert.Equal(t, "", result.WpaKey)
}

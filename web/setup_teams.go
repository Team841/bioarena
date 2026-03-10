// Copyright 2014 Team 254. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)
//
// Web routes for managing teams.

package web

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/Team254/cheesy-arena/model"
)

// Returns a single team as JSON.
func (web *Web) teamGetHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}

	teamId, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		handleWebErr(w, err)
		return
	}

	team, err := web.arena.Database.GetTeamById(teamId)
	if err != nil {
		handleWebErr(w, err)
		return
	}
	if team == nil {
		http.Error(w, "Team not found.", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err = json.NewEncoder(w).Encode(struct {
		Id     int    `json:"id"`
		Name   string `json:"name"`
		WpaKey string `json:"wpaKey"`
	}{team.Id, team.Name, team.WpaKey}); err != nil {
		handleWebErr(w, err)
	}
}

// Shows the team list page.
func (web *Web) teamsGetHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}

	web.renderTeams(w, r, "")
}

// Creates a new team.
func (web *Web) teamsAddHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}

	teamId, err := strconv.Atoi(r.PostFormValue("id"))
	if err != nil || teamId <= 0 {
		web.renderTeams(w, r, "Team number must be a positive integer.")
		return
	}

	existingTeam, err := web.arena.Database.GetTeamById(teamId)
	if err != nil {
		handleWebErr(w, err)
		return
	}
	if existingTeam != nil {
		web.renderTeams(w, r, "A team with that number already exists.")
		return
	}

	team := model.Team{
		Id:     teamId,
		Name:   r.PostFormValue("name"),
		WpaKey: r.PostFormValue("wpaKey"),
	}

	if err = web.arena.Database.CreateTeam(&team); err != nil {
		handleWebErr(w, err)
		return
	}

	http.Redirect(w, r, "/setup/teams", 303)
}

// Updates an existing team.
func (web *Web) teamsEditHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}

	teamId, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		handleWebErr(w, err)
		return
	}

	team, err := web.arena.Database.GetTeamById(teamId)
	if err != nil {
		handleWebErr(w, err)
		return
	}
	if team == nil {
		http.Error(w, "Team not found.", http.StatusNotFound)
		return
	}

	team.Name = r.PostFormValue("name")
	team.WpaKey = r.PostFormValue("wpaKey")

	if err = web.arena.Database.UpdateTeam(team); err != nil {
		handleWebErr(w, err)
		return
	}

	http.Redirect(w, r, "/setup/teams", 303)
}

// Deletes a team.
func (web *Web) teamsDeleteHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}

	teamId, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		handleWebErr(w, err)
		return
	}

	if err = web.arena.Database.DeleteTeam(teamId); err != nil {
		handleWebErr(w, err)
		return
	}

	http.Redirect(w, r, "/setup/teams", 303)
}

func (web *Web) renderTeams(w http.ResponseWriter, r *http.Request, errorMessage string) {
	template, err := web.parseFiles("templates/setup_teams.html", "templates/base.html")
	if err != nil {
		handleWebErr(w, err)
		return
	}
	teams, err := web.arena.Database.GetAllTeams()
	if err != nil {
		handleWebErr(w, err)
		return
	}
	data := struct {
		*model.EventSettings
		Teams        []model.Team
		ErrorMessage string
	}{web.arena.EventSettings, teams, errorMessage}
	if err = template.ExecuteTemplate(w, "base", data); err != nil {
		handleWebErr(w, err)
		return
	}
}

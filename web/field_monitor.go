// Copyright 2018 Team 254. All Rights Reserved.
// Portions Copyright Team 841. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)
//
// Web handlers for the field monitor: robot readiness and connection status.
//
// Adapted from upstream's field_monitor_display.go. Upstream serves two variants through
// the display registry, gated by `fta`, `ds` and `reversed` query parameters that the
// registry supplies, and its websocket also carries realtime scores, event status, and a
// command for writing FTA notes onto the team record.
//
// This fork has no display registry, no realtime score, no event-status notifier, and no
// notes column on the team model, so what is left is the part that matters on a practice
// field: the per-station diagnostic, fed from arena status, which bioarena already carries
// every value for.

package web

import (
	"net/http"

	"github.com/team841/bioarena/model"
	"github.com/team841/bioarena/websocket"
)

// Renders the field monitor.
//
// Behind the admin login, unlike the audience display. This one names teams and shows
// their robots' faults, and it is an operator's diagnostic rather than something for the
// room to read -- the same reasoning that puts Match Play behind it.
func (web *Web) fieldMonitorHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}

	template, err := web.parseFiles("templates/field_monitor.html")
	if err != nil {
		handleWebErr(w, err)
		return
	}

	data := struct {
		*model.EventSettings
	}{web.arena.EventSettings}
	if err = template.ExecuteTemplate(w, "field_monitor.html", data); err != nil {
		handleWebErr(w, err)
		return
	}
}

// The websocket endpoint for the field monitor to receive status updates.
func (web *Web) fieldMonitorWebsocketHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}

	ws, err := websocket.NewWebsocket(w, r)
	if err != nil {
		handleWebErr(w, err)
		return
	}
	defer ws.Close()

	// Every box on the page is drawn from arena status; the rest is the match clock in the
	// header. Upstream also subscribes to realtime scores and event status, neither of
	// which this fork has.
	ws.HandleNotifiers(
		web.arena.MatchTimingNotifier,
		web.arena.ArenaStatusNotifier,
		web.arena.MatchLoadNotifier,
		web.arena.MatchTimeNotifier,
	)
}

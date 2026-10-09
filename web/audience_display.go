// Copyright 2014 Team 254. All Rights Reserved.
// Portions Copyright Team 841. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)
//
// Web handlers for the audience display.
//
// Informed by upstream's audience display rather than ported from it. Upstream's is a
// chroma-key overlay: a strip across the bottom of a webcast, on a green background, with
// ten screens driven by an operator's radio buttons. This one fills a screen, because a
// practice field puts a television beside the field rather than streaming, and it has two
// screens that follow the match state on their own.
//
// Six of upstream's ten -- final score, bracket, alliance selection, sponsor reel, lower
// thirds, timeout -- need models this fork deleted, and its per-display configuration
// needs the display registry the fork also deleted. What is kept is what upstream's
// overlay shows during a match, which is exactly what bioarena now has the data for.

package web

import (
	"net/http"

	"github.com/team841/bioarena/model"
	"github.com/team841/bioarena/websocket"
)

// Renders the audience display.
//
// Deliberately unauthenticated, like /static/ and /logs/. This is a television on the
// field with no keyboard, usually started by the kiosk autostart; a login prompt is a
// screen nobody can clear. It shows the match clock and the FUEL tallies, which everyone
// watching the field can already see.
func (web *Web) audienceDisplayHandler(w http.ResponseWriter, r *http.Request) {
	template, err := web.parseFiles("templates/audience_display.html")
	if err != nil {
		handleWebErr(w, err)
		return
	}

	data := struct {
		*model.EventSettings
	}{web.arena.EventSettings}
	if err = template.ExecuteTemplate(w, "audience_display.html", data); err != nil {
		handleWebErr(w, err)
		return
	}
}

// The websocket endpoint for the audience display to receive match updates.
func (web *Web) audienceDisplayWebsocketHandler(w http.ResponseWriter, r *http.Request) {
	ws, err := websocket.NewWebsocket(w, r)
	if err != nil {
		handleWebErr(w, err)
		return
	}
	defer ws.Close()

	// No PlaySoundNotifier. This fork plays the match cues on the match panel, and a
	// television in the same room playing them half a second later is worse than a
	// television that stays quiet.
	ws.HandleNotifiers(
		web.arena.MatchTimingNotifier,
		web.arena.MatchLoadNotifier,
		web.arena.MatchTimeNotifier,
		web.arena.HubScoreNotifier,
	)
}

package web

import (
	"testing"

	gorillawebsocket "github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/team841/bioarena/websocket"
)

// The display is deliberately reachable without logging in. A television on the field has
// no keyboard, and a login prompt on it is a screen nobody can clear.
func TestAudienceDisplayNeedsNoLogin(t *testing.T) {
	web := setupTestWeb(t)

	recorder := web.getHttpResponse("/audience_display")

	assert.Equal(t, 200, recorder.Code)
	assert.NotContains(t, recorder.Body.String(), "Log In")
	assert.Contains(t, recorder.Body.String(), "matchScreen")
	assert.Contains(t, recorder.Body.String(), "logoScreen")
}

// Both screens are in the markup from the start; the client hides one. A display that had
// to fetch a screen on a match transition would show nothing while it did.
func TestAudienceDisplayRendersBothScreens(t *testing.T) {
	web := setupTestWeb(t)

	body := web.getHttpResponse("/audience_display").Body.String()

	for _, id := range []string{
		"clockText", "phaseText", "shiftText", "matchName",
		"fuel-red", "fuel-blue", "hub-red", "hub-blue", "teams-red", "teams-blue",
	} {
		assert.Contains(t, body, id, "the match screen is missing %s", id)
	}
	assert.Contains(t, body, "/static/js/audience_display.js")
	assert.Contains(t, body, "/static/css/audience_display.css")
}

// Everything the display draws has to arrive on connect, or a television switched on
// between matches sits blank until the next one starts.
func TestAudienceDisplayWebsocketSendsTheCurrentState(t *testing.T) {
	web := setupTestWeb(t)

	server, wsUrl := web.startTestServer()
	defer server.Close()
	conn, _, err := gorillawebsocket.DefaultDialer.Dial(wsUrl+"/audience_display/websocket", nil)
	assert.Nil(t, err)
	defer conn.Close()
	ws := websocket.NewTestWebsocket(conn)

	readWebsocketType(t, ws, "matchTiming")
	readWebsocketType(t, ws, "matchLoad")
	readWebsocketType(t, ws, "matchTime")
	readWebsocketType(t, ws, "hubScore")
}

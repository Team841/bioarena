package web

import (
	"testing"

	gorillawebsocket "github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/team841/bioarena/websocket"
)

// Unlike the audience display, this one names teams and shows their robots' faults, so it
// sits behind the same login as Match Play.
//
// A password has to be set to see that: with none configured, userIsAdmin lets everything
// through, which is why the rest of these tests reach the page without a cookie.
func TestFieldMonitorRequiresLogin(t *testing.T) {
	web := setupTestWeb(t)
	web.arena.EventSettings.AdminPassword = "sekrit"

	recorder := web.getHttpResponse("/field_monitor")

	assert.Equal(t, 307, recorder.Code)
	assert.Contains(t, recorder.Header().Get("Location"), "/login")
}

func TestFieldMonitorRendersEveryStation(t *testing.T) {
	web := setupTestWeb(t)

	body := web.getHttpResponse("/field_monitor").Body.String()

	// Three rows, mirrored, so each side carries positions 1 through 3.
	for _, side := range []string{"left", "right"} {
		for _, position := range []string{"1", "2", "3"} {
			prefix := side + "Team" + position
			for _, box := range []string{"Id", "Ethernet", "Ds", "Radio", "Robot", "Battery",
				"Bandwidth", "TripTime", "MissedPackets", "Bypass"} {
				assert.Contains(t, body, prefix+box, "missing %s", prefix+box)
			}
		}
	}
	assert.Contains(t, body, "accessPointStatus")
	assert.Contains(t, body, "switchStatus")
	assert.Contains(t, body, "/static/js/field_monitor_display.js")
}

// A page of green status boxes frozen at the field's last word looks exactly like a
// healthy field, so the markup has to carry something that can say otherwise.
func TestFieldMonitorCarriesTheOfflineBanner(t *testing.T) {
	web := setupTestWeb(t)

	body := web.getHttpResponse("/field_monitor").Body.String()

	assert.Contains(t, body, "offlineBanner")
	assert.Contains(t, body, "NO CONNECTION TO THE FIELD")
}

// Upstream's page also renders the realtime scores and a cycle-time footer. This fork has
// neither, and the markup must not be left asking for them.
func TestFieldMonitorDropsWhatThisForkHasNoDataFor(t *testing.T) {
	web := setupTestWeb(t)

	body := web.getHttpResponse("/field_monitor").Body.String()

	for _, absent := range []string{"leftScore", "rightScore", "cycleTimeMessage",
		"earlyLateMessage", "Notes"} {
		assert.NotContains(t, body, absent, "%s should have gone with its data", absent)
	}
}

// Everything the page draws has to arrive on connect, or a monitor opened between matches
// sits blank until something changes.
func TestFieldMonitorWebsocketSendsTheCurrentState(t *testing.T) {
	web := setupTestWeb(t)

	server, wsUrl := web.startTestServer()
	defer server.Close()
	conn, _, err := gorillawebsocket.DefaultDialer.Dial(wsUrl+"/field_monitor/websocket", nil)
	assert.Nil(t, err)
	defer conn.Close()
	ws := websocket.NewTestWebsocket(conn)

	readWebsocketType(t, ws, "matchTiming")
	readWebsocketType(t, ws, "arenaStatus")
	readWebsocketType(t, ws, "matchLoad")
	readWebsocketType(t, ws, "matchTime")
}

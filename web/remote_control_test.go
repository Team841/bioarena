// Portions Copyright Team 841. All Rights Reserved.

package web

import (
	"net/http"
	"strings"
	"testing"
	"time"

	gorillawebsocket "github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/team841/bioarena/field"
	"github.com/team841/bioarena/model"
	"github.com/team841/bioarena/websocket"
)

const testRemoteKey = "test-remote-key"

func enableRemoteControl(web *Web, allowStart bool) {
	web.arena.EventSettings.RemoteControlAddress = "127.0.0.1"
	web.arena.EventSettings.RemoteControlKey = testRemoteKey
	web.arena.EventSettings.RemoteControlAllowStart = allowStart
}

func dialRemote(t *testing.T, wsUrl, key string) (*gorillawebsocket.Conn, *http.Response, error) {
	header := http.Header{}
	header.Set(remoteControlKeyHeader, key)
	return gorillawebsocket.DefaultDialer.Dial(wsUrl+"/remote_control/websocket", header)
}

// connectRemote returns a connection past its opening status messages.
func connectRemote(t *testing.T, wsUrl string) *websocket.Websocket {
	conn, _, err := dialRemote(t, wsUrl, testRemoteKey)
	assert.Nil(t, err)
	t.Cleanup(func() { conn.Close() })
	ws := websocket.NewTestWebsocket(conn)
	readWebsocketType(t, ws, "matchTiming")
	readWebsocketType(t, ws, "arenaStatus")
	readWebsocketType(t, ws, "matchLoad")
	readWebsocketType(t, ws, "matchTime")
	return ws
}

var remoteCommandId int

// sendRemote runs one command and returns the field's error text, "" for success.
func sendRemote(t *testing.T, ws *websocket.Websocket, kind string, args map[string]any) string {
	remoteCommandId++
	sent := map[string]any{"Id": remoteCommandId}
	for k, v := range args {
		sent[k] = v
	}
	assert.Nil(t, ws.Write(kind, sent))
	for {
		messageType, data, err := ws.ReadWithTimeout(2 * time.Second)
		if !assert.Nil(t, err) {
			return "no result"
		}
		if messageType == "result" {
			result := data.(map[string]any)
			assert.Equal(t, float64(remoteCommandId), result["Id"])
			return result["Error"].(string)
		}
	}
}

func lineup(red1, red2, red3, blue1, blue2, blue3 int) map[string]any {
	return map[string]any{"Red1": red1, "Red2": red2, "Red3": red3, "Blue1": blue1, "Blue2": blue2, "Blue3": blue3}
}

func TestRemoteControlRefusedUnlessConfigured(t *testing.T) {
	web := setupTestWeb(t)
	server, wsUrl := web.startTestServer()
	defer server.Close()

	// Off by default.
	_, response, err := dialRemote(t, wsUrl, testRemoteKey)
	assert.NotNil(t, err)
	assert.Equal(t, 403, response.StatusCode)

	// Right address, wrong key.
	enableRemoteControl(web, true)
	_, response, err = dialRemote(t, wsUrl, "not-the-key")
	assert.NotNil(t, err)
	assert.Equal(t, 403, response.StatusCode)

	// Right key, wrong address.
	web.arena.EventSettings.RemoteControlAddress = "10.0.100.21"
	_, response, err = dialRemote(t, wsUrl, testRemoteKey)
	assert.NotNil(t, err)
	assert.Equal(t, 403, response.StatusCode)

	// The Match Play socket's login is not a way in either: no key, no connection.
	web.arena.EventSettings.RemoteControlAddress = "127.0.0.1"
	_, response, err = gorillawebsocket.DefaultDialer.Dial(wsUrl+"/remote_control/websocket", nil)
	assert.NotNil(t, err)
	assert.Equal(t, 403, response.StatusCode)
}

func TestRemoteControlStatusReachesTheOperator(t *testing.T) {
	web := setupTestWeb(t)
	assert.Equal(t, field.RemoteControlNotConfigured, web.arena.RemoteControlStatus())
	enableRemoteControl(web, false)
	assert.Equal(t, field.RemoteControlOffline, web.arena.RemoteControlStatus())

	server, wsUrl := web.startTestServer()
	defer server.Close()
	connectRemote(t, wsUrl)
	assert.Equal(t, field.RemoteControlOnline, web.arena.RemoteControlStatus())
}

func TestRemoteControlRevokedConnectionCloses(t *testing.T) {
	web := setupTestWeb(t)
	enableRemoteControl(web, true)
	server, wsUrl := web.startTestServer()
	defer server.Close()
	ws := connectRemote(t, wsUrl)

	web.arena.EventSettings.RemoteControlAddress = ""
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, _, err := ws.ReadWithTimeout(time.Second); err != nil {
			return
		}
	}
	t.Fatal("connection stayed open after remote control was turned off")
}

func TestRemoteLoadLineupAddsTeamsAndClearsStaleBypass(t *testing.T) {
	web := setupTestWeb(t)
	enableRemoteControl(web, true)
	server, wsUrl := web.startTestServer()
	defer server.Close()
	ws := connectRemote(t, wsUrl)

	// Left on from an earlier match, on one station that gets a team and one that doesn't.
	web.arena.AllianceStations["R2"].Bypass.Store(true)
	web.arena.AllianceStations["B3"].Bypass.Store(true)

	args := lineup(9101, 9102, 9103, 9104, 9105, 0)
	args["Name"] = "qm7"
	assert.Equal(t, "", sendRemote(t, ws, "loadLineup", args))

	team, err := web.arena.Database.GetTeamById(9101)
	assert.Nil(t, err)
	assert.Equal(t, "00009101", team.WpaKey)
	assert.Equal(t, 9105, web.arena.CurrentMatch.Blue2)
	assert.Equal(t, "qm7", web.arena.CurrentMatch.LongName)
	assert.False(t, web.arena.AllianceStations["R2"].Bypass.Load())
	assert.True(t, web.arena.AllianceStations["B3"].Bypass.Load())

	// An existing team keeps its key.
	assert.Nil(t, web.arena.Database.UpdateTeam(&model.Team{Id: 9101, WpaKey: "customkey"}))
	assert.Equal(t, "", sendRemote(t, ws, "loadLineup", args))
	team, _ = web.arena.Database.GetTeamById(9101)
	assert.Equal(t, "customkey", team.WpaKey)

	web.arena.MatchState = field.AutoPeriod
	assert.Contains(t, sendRemote(t, ws, "loadLineup", args), "between matches")
	web.arena.MatchState = field.PreMatch
}

func bypassAll(web *Web) {
	for _, station := range web.arena.AllianceStations {
		station.Bypass.Store(true)
	}
}

func TestRemoteStartNeedsPermissionAndTheSameLineup(t *testing.T) {
	web := setupTestWeb(t)
	enableRemoteControl(web, false)
	server, wsUrl := web.startTestServer()
	defer server.Close()
	ws := connectRemote(t, wsUrl)

	teams := lineup(9101, 9102, 9103, 9104, 9105, 9106)
	assert.Equal(t, "", sendRemote(t, ws, "loadLineup", teams))
	bypassAll(web)

	assert.Contains(t, sendRemote(t, ws, "startMatch", teams), "only allows matches to be started from its own")
	assert.Equal(t, field.PreMatch, web.arena.MatchState)

	web.arena.EventSettings.RemoteControlAllowStart = true
	assert.Contains(t, sendRemote(t, ws, "startMatch", lineup(9101, 9102, 9103, 9104, 9105, 9199)), "has changed")
	assert.Equal(t, field.PreMatch, web.arena.MatchState)

	assert.Equal(t, "", sendRemote(t, ws, "startMatch", teams))
	assert.Equal(t, field.StartMatch, web.arena.MatchState)

	assert.Equal(t, "", sendRemote(t, ws, "abortMatch", nil))
	assert.Equal(t, field.PostMatch, web.arena.MatchState)
	assert.Contains(t, sendRemote(t, ws, "abortMatch", nil), "not in progress")
}

func TestRemoteStartRefusalSaysWhy(t *testing.T) {
	web := setupTestWeb(t)
	enableRemoteControl(web, true)
	server, wsUrl := web.startTestServer()
	defer server.Close()
	ws := connectRemote(t, wsUrl)
	teams := lineup(9101, 9102, 9103, 9104, 9105, 9106)
	assert.Equal(t, "", sendRemote(t, ws, "loadLineup", teams))

	problem := sendRemote(t, ws, "startMatch", teams)
	assert.Contains(t, problem, "robots are connected or bypassed")
	assert.NotEqual(t, field.StartMatch, web.arena.MatchState)
}

func TestRemoteEStopIsSetNotToggled(t *testing.T) {
	web := setupTestWeb(t)
	enableRemoteControl(web, true)
	server, wsUrl := web.startTestServer()
	defer server.Close()
	ws := connectRemote(t, wsUrl)
	estop := map[string]any{"Station": "R2", "On": true}

	for i := 0; i < 3; i++ {
		assert.Equal(t, "", sendRemote(t, ws, "setEStop", estop))
		assert.True(t, web.arena.AllianceStations["R2"].EStop.Load())
	}

	assert.Equal(t, "", sendRemote(t, ws, "setEStop", map[string]any{"Station": "R2", "On": false}))
	assert.False(t, web.arena.AllianceStations["R2"].EStop.Load())
	assert.Contains(t, sendRemote(t, ws, "setEStop", map[string]any{"Station": "R9", "On": true}), "Invalid alliance station")
}

func TestRemoteStopsWorkDuringAMatchButCannotBeUndone(t *testing.T) {
	web := setupTestWeb(t)
	enableRemoteControl(web, true)
	server, wsUrl := web.startTestServer()
	defer server.Close()
	ws := connectRemote(t, wsUrl)
	web.arena.MatchState = field.TeleopPeriod
	defer func() { web.arena.MatchState = field.PreMatch }()

	assert.Equal(t, "", sendRemote(t, ws, "setEStop", map[string]any{"Station": "B1", "On": true}))
	assert.True(t, web.arena.AllianceStations["B1"].EStop.Load())
	assert.Contains(t, sendRemote(t, ws, "setEStop", map[string]any{"Station": "B1", "On": false}), "in progress")
	assert.True(t, web.arena.AllianceStations["B1"].EStop.Load())

	assert.Equal(t, "", sendRemote(t, ws, "setBypass", map[string]any{"Station": "B2", "On": true}))
	assert.True(t, web.arena.AllianceStations["B2"].Bypass.Load())
	assert.Contains(t, sendRemote(t, ws, "setBypass", map[string]any{"Station": "B2", "On": false}), "in progress")
	assert.True(t, web.arena.AllianceStations["B2"].Bypass.Load())

	assert.Contains(t, sendRemote(t, ws, "clearFieldEStop", nil), "in progress")
	assert.Contains(t, sendRemote(t, ws, "setAutoWinnerMode", map[string]any{"Mode": "red"}), "in progress")
	assert.Contains(t, sendRemote(t, ws, "launchMissiles", nil), "Invalid message type")
}

func TestRemoteAutoWinnerMode(t *testing.T) {
	web := setupTestWeb(t)
	enableRemoteControl(web, true)
	server, wsUrl := web.startTestServer()
	defer server.Close()
	ws := connectRemote(t, wsUrl)

	assert.Equal(t, "", sendRemote(t, ws, "setAutoWinnerMode", map[string]any{"Mode": "red"}))
	assert.Equal(t, "red", web.arena.AutoWinnerMode.String())
	assert.Contains(t, sendRemote(t, ws, "setAutoWinnerMode", map[string]any{"Mode": "nonsense"}), "invalid AUTO winner mode")
}

func TestRemoteControlSettingsValidation(t *testing.T) {
	settings := &model.EventSettings{}
	assert.Nil(t, checkRemoteControl(settings))
	assert.Equal(t, "", settings.RemoteControlKey)

	settings.RemoteControlAddress = "watchtower.local"
	assert.Contains(t, checkRemoteControl(settings).Error(), "IP address")

	settings.RemoteControlAddress = "10.0.100.21"
	assert.Nil(t, checkRemoteControl(settings))
	assert.Equal(t, 16, len(settings.RemoteControlKey))
	assert.False(t, strings.Contains(settings.RemoteControlKey, "-"))

	settings.RemoteControlKey = "short"
	assert.Contains(t, checkRemoteControl(settings).Error(), "at least")

	settings.RemoteControlKey = "chosen-by-hand"
	assert.Nil(t, checkRemoteControl(settings))
	assert.Equal(t, "chosen-by-hand", settings.RemoteControlKey)
}

func TestSettingsPageSavesRemoteControl(t *testing.T) {
	web := setupTestWeb(t)
	recorder := web.getHttpResponse("/setup/settings")
	assert.Contains(t, recorder.Body.String(), "Watchtower Remote Control")

	recorder = web.postHttpResponse("/setup/settings",
		"remoteControlAddress=10.0.100.21&remoteControlKey=&remoteControlAllowStart=on&autoDurationSec=20&pauseDurationSec=3&teleopDurationSec=140&warningRemainingDurationSec=30")
	assert.Equal(t, 303, recorder.Code)
	assert.Equal(t, "10.0.100.21", web.arena.EventSettings.RemoteControlAddress)
	assert.True(t, web.arena.EventSettings.RemoteControlAllowStart)
	key := web.arena.EventSettings.RemoteControlKey
	assert.Equal(t, 16, len(key))

	recorder = web.postHttpResponse("/setup/settings", "remoteControlAddress=not-an-address&autoDurationSec=20")
	assert.Contains(t, recorder.Body.String(), "IP address")
}

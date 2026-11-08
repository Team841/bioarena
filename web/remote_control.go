// Portions Copyright Team 841. All Rights Reserved.
//
// Watchtower remote control: one websocket that lets a scoring FMS on the network run this
// field. See docs/watchtower-remote-control.md for the protocol.
//
// It is separate from /match_play/websocket so that Watchtower never holds the admin
// password, which would also open Settings and the database, and so the field's operator
// turns it on deliberately. Stops and bypasses are set to a value here rather than toggled.

package web

import (
	"crypto/subtle"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mitchellh/mapstructure"
	"github.com/team841/bioarena/field"
	"github.com/team841/bioarena/model"
	"github.com/team841/bioarena/websocket"
)

const (
	remoteControlKeyHeader = "X-Remote-Key"
	minRemoteControlKeyLen = 8

	// How often a connection re-checks that it is still allowed, so turning remote control
	// off in Settings takes effect on a connection that is already open.
	remoteControlRecheckPeriod = time.Second
)

// remoteCommand is every argument any command takes; each uses the ones it needs.
type remoteCommand struct {
	Id      int
	Station string
	On      bool
	Mode    string
	Name    string
	Red1    int
	Red2    int
	Red3    int
	Blue1   int
	Blue2   int
	Blue3   int
}

func (args remoteCommand) teams() [6]int {
	return [6]int{args.Red1, args.Red2, args.Red3, args.Blue1, args.Blue2, args.Blue3}
}

// checkRemoteControl validates the settings, making a key if one is needed and none was
// given. As with the FUEL counter, a blank address is off, and anything else must be an
// address: connections are accepted by comparing the source against this value.
func checkRemoteControl(settings *model.EventSettings) error {
	if settings.RemoteControlAddress == "" {
		return nil
	}
	if net.ParseIP(settings.RemoteControlAddress) == nil {
		return fmt.Errorf(
			"Watchtower address must be an IP address, not a hostname: %q.", settings.RemoteControlAddress,
		)
	}
	if settings.RemoteControlKey == "" {
		settings.RemoteControlKey = strings.ReplaceAll(uuid.New().String(), "-", "")[:16]
	}
	if len(settings.RemoteControlKey) < minRemoteControlKeyLen {
		return fmt.Errorf("Watchtower key must be at least %d characters.", minRemoteControlKeyLen)
	}
	return nil
}

// remoteControlAuthorized is the whole access check: the configured source address and key.
func (web *Web) remoteControlAuthorized(remoteAddr, key string) bool {
	settings := web.arena.EventSettings
	if settings.RemoteControlAddress == "" || settings.RemoteControlKey == "" {
		return false
	}
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return false
	}
	source, allowed := net.ParseIP(host), net.ParseIP(settings.RemoteControlAddress)
	if source == nil || allowed == nil || !source.Equal(allowed) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(key), []byte(settings.RemoteControlKey)) == 1
}

// The websocket endpoint for Watchtower to send commands and receive the field's status.
func (web *Web) remoteControlWebsocketHandler(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get(remoteControlKeyHeader)
	if !web.remoteControlAuthorized(r.RemoteAddr, key) {
		http.Error(w, "Remote control is not allowed from this address with this key.", http.StatusForbidden)
		return
	}

	ws, err := websocket.NewWebsocket(w, r)
	if err != nil {
		handleWebErr(w, err)
		return
	}
	defer ws.Close()
	defer web.arena.RemoteControlConnected()()

	go ws.HandleNotifiers(
		web.arena.MatchTimingNotifier,
		web.arena.ArenaStatusNotifier,
		web.arena.MatchLoadNotifier,
		web.arena.MatchTimeNotifier,
	)

	done := make(chan struct{})
	defer close(done)
	go func() {
		ticker := time.NewTicker(remoteControlRecheckPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if !web.remoteControlAuthorized(r.RemoteAddr, key) {
					ws.Close()
					return
				}
			}
		}
	}()

	for {
		messageType, data, err := ws.Read()
		if err != nil {
			if err != io.EOF {
				log.Println(err)
			}
			return
		}
		if !web.remoteControlAuthorized(r.RemoteAddr, key) {
			return
		}

		var args remoteCommand
		if err = mapstructure.Decode(data, &args); err == nil {
			err = web.runRemoteCommand(messageType, args)
		}

		// Status first, so that Watchtower's view is current by the time it reads the result.
		if writeErr := ws.WriteNotifier(web.arena.ArenaStatusNotifier); writeErr != nil {
			log.Println(writeErr)
		}
		result := struct {
			Id    int
			Error string
		}{Id: args.Id}
		if err != nil {
			result.Error = err.Error()
		}
		if writeErr := ws.Write("result", result); writeErr != nil {
			log.Println(writeErr)
		}
	}
}

func (web *Web) runRemoteCommand(messageType string, args remoteCommand) error {
	arena := web.arena
	switch messageType {
	case "loadLineup":
		return web.remoteLoadLineup(args)
	case "startMatch":
		if !arena.EventSettings.RemoteControlAllowStart {
			return fmt.Errorf("This field only allows matches to be started from its own Match Play page.")
		}
		// Named by Watchtower because it scores that lineup: someone may have changed the
		// field's since it was loaded, and those teams would be enabled and scored wrongly.
		current := arena.CurrentMatch
		if args.teams() != [6]int{current.Red1, current.Red2, current.Red3, current.Blue1, current.Blue2, current.Blue3} {
			return fmt.Errorf("The lineup on the field has changed. Load it again.")
		}
		return arena.StartMatch()
	case "abortMatch":
		return arena.AbortMatch()
	case "setEStop":
		return arena.SetStationEStop(args.Station, args.On)
	case "setBypass":
		return arena.SetStationBypass(args.Station, args.On)
	case "bypassEmptyStations":
		arena.BypassEmptyStations()
		return nil
	case "clearFieldEStop":
		if arena.MatchState != field.PreMatch && arena.MatchState != field.PostMatch {
			return fmt.Errorf("Cannot clear the field e-stop while a match is in progress.")
		}
		arena.ClearFieldEStop()
		return nil
	case "setAutoWinnerMode":
		mode, err := field.ParseAutoWinnerMode(args.Mode)
		if err != nil {
			return err
		}
		if err = arena.SetAutoWinnerMode(mode); err != nil {
			return err
		}
		arena.MatchLoadNotifier.Notify()
		return nil
	}
	return fmt.Errorf("Invalid message type '%s'.", messageType)
}

// remoteLoadLineup puts a match's teams on the field, adding any it has never seen.
func (web *Web) remoteLoadLineup(args remoteCommand) error {
	arena := web.arena
	if arena.MatchState != field.PreMatch {
		return fmt.Errorf("The field only takes a new lineup between matches.")
	}

	teams := args.teams()
	for _, teamId := range teams {
		if teamId == 0 {
			continue
		}
		team, err := arena.Database.GetTeamById(teamId)
		if err != nil {
			return err
		}
		if team == nil {
			if err = arena.Database.CreateTeam(newQuickAddTeam(teamId)); err != nil {
				return err
			}
		}
	}

	if err := arena.SubstituteTeams(teams[0], teams[1], teams[2], teams[3], teams[4], teams[5]); err != nil {
		return err
	}

	// A bypass carries across matches for a practice field running one lineup. With a new
	// lineup that would leave a robot silently disabled in a station that now has a team.
	for i, station := range [6]string{"R1", "R2", "R3", "B1", "B2", "B3"} {
		if teams[i] != 0 {
			arena.AllianceStations[station].Bypass.Store(false)
		}
	}

	if arena.CurrentMatch.Type == model.Test && args.Name != "" {
		arena.CurrentMatch.LongName = args.Name
		arena.MatchLoadNotifier.Notify()
	}
	return nil
}

// Portions Copyright Team 841. All Rights Reserved.
//
// Arena state for Watchtower remote control; the protocol is in web/remote_control.go.

package field

import "fmt"

const (
	RemoteControlOnline        = "ONLINE"
	RemoteControlOffline       = "OFFLINE"
	RemoteControlNotConfigured = "NOT CONFIGURED"
)

// RemoteControlConnected records a connection and returns the function that ends it.
func (arena *Arena) RemoteControlConnected() func() {
	arena.remoteControlClients.Add(1)
	return func() { arena.remoteControlClients.Add(-1) }
}

// RemoteControlStatus is what the match panel says about Watchtower.
func (arena *Arena) RemoteControlStatus() string {
	if arena.EventSettings.RemoteControlAddress == "" {
		return RemoteControlNotConfigured
	}
	if arena.remoteControlClients.Load() > 0 {
		return RemoteControlOnline
	}
	return RemoteControlOffline
}

// matchInProgress is whether robots could be enabled by the running match. Hold arena.mu.
func (arena *Arena) matchInProgress() bool {
	return arena.MatchState != PreMatch && arena.MatchState != PostMatch
}

// SetStationEStop sets a station's e-stop to a value, where the Match Play page toggles one.
// A toggle over a network is unsafe: a repeated command would release the stop. Releasing
// is refused during a match, since that is the direction that can re-enable a robot.
func (arena *Arena) SetStationEStop(station string, on bool) error {
	return arena.setStationFlag(station, on, "clear an e-stop", func(as *AllianceStation, value bool) {
		as.EStop.Store(value)
	})
}

// SetStationBypass sets a station's bypass to a value. Bypassing disables a robot, so it is
// allowed at any time; removing one mid-match would enable it, and is refused.
func (arena *Arena) SetStationBypass(station string, on bool) error {
	return arena.setStationFlag(station, on, "remove a bypass", func(as *AllianceStation, value bool) {
		as.Bypass.Store(value)
	})
}

func (arena *Arena) setStationFlag(station string, on bool, undo string, set func(*AllianceStation, bool)) error {
	arena.mu.Lock()
	defer arena.mu.Unlock()

	allianceStation, ok := arena.AllianceStations[station]
	if !ok {
		return fmt.Errorf("Invalid alliance station '%s'.", station)
	}
	if !on && arena.matchInProgress() {
		return fmt.Errorf("Cannot %s while a match is in progress.", undo)
	}
	set(allianceStation, on)
	return nil
}

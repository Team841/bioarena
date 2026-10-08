// Copyright 2018 Team 254. All Rights Reserved.
// Portions Copyright Team 841. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)
//
// Contains configuration of the publish-subscribe notifiers that allow the arena to push updates to websocket clients.

package field

import (
	"time"

	"github.com/team841/bioarena/game"
	"github.com/team841/bioarena/hardware"
	"github.com/team841/bioarena/model"
	"github.com/team841/bioarena/network"
	"github.com/team841/bioarena/websocket"
)

// faultDescription renders a stored hardware.FaultKind for the web UI,
// returning "" for a healthy input so the JavaScript can test it directly.
func faultDescription(kind uint32) string {
	if hardware.FaultKind(kind) == hardware.FaultNone {
		return ""
	}
	return hardware.FaultKind(kind).String()
}

type ArenaNotifiers struct {
	ArenaStatusNotifier *websocket.Notifier
	MatchLoadNotifier   *websocket.Notifier
	MatchTimeNotifier   *websocket.Notifier
	MatchTimingNotifier *websocket.Notifier
	PlaySoundNotifier   *websocket.Notifier
	// HubScoreNotifier carries the FUEL tallies. Separate from arenaStatus, which goes out
	// every 500ms: half a second between a shot and the tally moving is visible, and the
	// counter feed is specified to put it on screen inside 350ms. Fired on change instead,
	// which costs nothing between shots.
	HubScoreNotifier *websocket.Notifier
}

type MatchTimeMessage struct {
	MatchState
	MatchTimeSec int
	// Which HUBs would credit a FUEL arriving now, for the field banner. Carried on the
	// clock rather than on arenaStatus because every shift boundary falls on a whole
	// second, which is exactly when this notifier fires -- so the banner changes with the
	// field instead of up to half a second after it.
	HubShift      string
	RedHubActive  bool
	BlueHubActive bool
	InShift       bool
}

// allianceStationView is a JSON-safe projection of AllianceStation.
// AllianceStation uses atomic.Bool for EStop/AStop/Bypass, which serializes as {} in JSON.
// This struct materialises those values so the JavaScript UI receives correct booleans.
type allianceStationView struct {
	DsConn     *DriverStationConnection
	Ethernet   bool
	AStop      bool
	EStop      bool
	Bypass     bool
	Team       *model.Team
	WifiStatus network.TeamWifiStatus
	// EStopFault describes a dual-channel wiring fault on this station's e-stop,
	// or is empty when the wiring reads healthy. A faulted station also has
	// EStop set, so a UI that ignores this field still shows the stop.
	EStopFault string
	// PortLinked is whether this station's driver station port has link, from the switch.
	// Meaningless unless StationLinksKnown is set on the enclosing message.
	PortLinked bool
}

// Instantiates notifiers and configures their message producing methods.
func (arena *Arena) configureNotifiers() {
	arena.ArenaStatusNotifier = websocket.NewNotifier("arenaStatus", arena.generateArenaStatusMessage)
	arena.MatchLoadNotifier = websocket.NewNotifier("matchLoad", arena.GenerateMatchLoadMessage)
	arena.MatchTimeNotifier = websocket.NewNotifier("matchTime", arena.generateMatchTimeMessage)
	arena.MatchTimingNotifier = websocket.NewNotifier("matchTiming", arena.generateMatchTimingMessage)
	arena.PlaySoundNotifier = websocket.NewNotifier("playSound", nil)
	arena.HubScoreNotifier = websocket.NewNotifier("hubScore", arena.generateHubScoreMessage)
}

func (arena *Arena) generateArenaStatusMessage() any {
	stationViews := make(map[string]allianceStationView, len(arena.AllianceStations))
	for k, as := range arena.AllianceStations {
		stationViews[k] = allianceStationView{
			DsConn:     as.DsConn,
			Ethernet:   as.Ethernet,
			AStop:      as.AStop.Load(),
			EStop:      as.EStop.Load(),
			Bypass:     as.Bypass.Load(),
			Team:       as.Team,
			WifiStatus: as.WifiStatus,
			EStopFault: faultDescription(as.EStopFault.Load()),
			PortLinked: as.PortLinked.Load(),
		}
	}
	return &struct {
		MatchId          int
		AllianceStations map[string]allianceStationView
		MatchState
		CanStartMatch         bool
		AccessPointStatus     string
		SwitchStatus          string
		PlcIsHealthy          bool
		FieldEStop            bool
		PlcArmorBlockStatuses map[string]bool
		GpioFieldEStopActive  bool
		GpioFieldEStopFault   string
		// FieldEStopMonitored distinguishes a healthy button from no button at all. The
		// noop panel reports StopOK forever, so without this the badge reads the same
		// whether the field e-stop is working or was never configured.
		FieldEStopMonitored bool
		// Per-alliance e-stop panel reachability, in the access point's and switch's
		// vocabulary so the badges share their colours. DISABLED when no panel is
		// configured for that alliance.
		RedPanelStatus    string
		BluePanelStatus   string
		StationLinksKnown bool
		// HubCounterStatus is ONLINE, OFFLINE, or NOT CONFIGURED. Only the reachability
		// rides arenaStatus; the tallies themselves go out on hubScore, which fires on
		// change rather than every 500ms.
		HubCounterStatus string
	}{
		arena.CurrentMatch.Id,
		stationViews,
		arena.MatchState,
		arena.checkCanStartMatch() == nil,
		arena.accessPoint.Status,
		arena.teamNetwork.GetStatus(),
		arena.Plc.IsHealthy(),
		arena.Plc.GetFieldEStop(),
		arena.Plc.GetArmorBlockStatuses(),
		arena.fieldEStopActive.Load(),
		faultDescription(arena.fieldEStopFault.Load()),
		arena.fieldEStopMonitored.Load(),
		arena.panelStatus("red"),
		arena.panelStatus("blue"),
		arena.stationLinksKnown.Load(),
		arena.HubCounterStatus(),
	}
}

func (arena *Arena) GenerateMatchLoadMessage() any {
	teams := make(map[string]*model.Team)
	for station, allianceStation := range arena.AllianceStations {
		teams[station] = allianceStation.Team
	}

	matchResult, _ := arena.Database.GetMatchResultForMatch(arena.CurrentMatch.Id)
	isReplay := matchResult != nil

	return &struct {
		Match             *model.Match
		AllowSubstitution bool
		IsReplay          bool
		Teams             map[string]*model.Team
		AutoWinnerMode    string
	}{
		arena.CurrentMatch,
		arena.CurrentMatch.ShouldAllowSubstitution(),
		isReplay,
		teams,
		arena.AutoWinnerMode.String(),
	}
}

func (arena *Arena) generateMatchTimeMessage() any {
	shiftName, redActive, blueActive, inShift := arena.activeHubs(time.Now())
	return MatchTimeMessage{
		MatchState:    arena.MatchState,
		MatchTimeSec:  int(arena.MatchTimeSec()),
		HubShift:      shiftName,
		RedHubActive:  redActive,
		BlueHubActive: blueActive,
		InShift:       inShift,
	}
}

func (arena *Arena) generateMatchTimingMessage() any {
	return &game.MatchTiming
}

// panelStatus reports the reachability of the e-stop panel for one alliance, or DISABLED
// when none is configured. Panels are identified by what they report about themselves
// rather than by position in EStopPanels, which carries no order.
func (arena *Arena) panelStatus(alliance string) string {
	for _, panel := range arena.EStopPanels {
		reporter, ok := panel.(hardware.PanelStatusReporter)
		if ok && reporter.Alliance() == alliance {
			return reporter.Status()
		}
	}
	return hardware.PanelStatusDisabled
}

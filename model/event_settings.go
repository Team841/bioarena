// Copyright 2014 Team 254. All Rights Reserved.
// Portions Copyright Team 841. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)
//
// Model and datastore read/write methods for event-level configuration.

package model

import (
	"github.com/team841/bioarena/game"
	"github.com/team841/bioarena/hardware"
)

type PlayoffType int

const (
	DoubleEliminationPlayoff PlayoffType = iota
	SingleEliminationPlayoff
)

type EventSettings struct {
	Id                         int `db:"id"`
	Name                       string
	PlayoffType                PlayoffType
	NumPlayoffAlliances        int
	SelectionRound2Order       string
	SelectionRound3Order       string
	SelectionShowUnpickedTeams bool
	TbaDownloadEnabled         bool
	TbaPublishingEnabled       bool
	TbaEventCode               string
	TbaSecretId                string
	TbaSecret                  string
	NexusEnabled               bool
	NetworkSecurityEnabled     bool
	ApAddress                  string
	ApPassword                 string
	ApChannel                  int
	SwitchAddress              string
	SwitchPassword             string
	// SwitchDnsServer is handed to team subnets in the per-match DHCP pools. Blank omits
	// the option entirely, which is correct for a field with no upstream resolver: a
	// DNS server clients cannot reach makes lookups time out rather than fail fast.
	SwitchDnsServer       string
	RedEStopPanelAddress  string
	BlueEStopPanelAddress string
	// RedEStopPanelFieldEStop and BlueEStopPanelFieldEStop are whether that panel has a
	// field e-stop button fitted. Off, its field e-stop input is ignored: the panel reads the
	// pin regardless, and on a normally-closed input an empty pin reads as pressed.
	RedEStopPanelFieldEStop  bool
	BlueEStopPanelFieldEStop bool
	// FieldEStopPin is the BCM pin carrying the field e-stop's NO contact, and
	// FieldEStopNcPin the NC contact. Both set, the two channels are compared and
	// a disagreement is reported as a wiring fault; FieldEStopNcPin left at 0
	// falls back to single-channel monitoring with no fault detection.
	FieldEStopPin                    int
	FieldEStopNcPin                  int
	AdminPassword                    string
	TeamSignRed1Id                   int
	TeamSignRed2Id                   int
	TeamSignRed3Id                   int
	TeamSignRedTimerId               int
	TeamSignBlue1Id                  int
	TeamSignBlue2Id                  int
	TeamSignBlue3Id                  int
	TeamSignBlueTimerId              int
	UseLiteUdpPort                   bool
	BlackmagicAddresses              string
	CompanionAddress                 string
	CompanionPort                    int
	CompanionMatchPreviewPage        int
	CompanionMatchPreviewRow         int
	CompanionMatchPreviewColumn      int
	CompanionSetAudiencePage         int
	CompanionSetAudienceRow          int
	CompanionSetAudienceColumn       int
	CompanionMatchStartPage          int
	CompanionMatchStartRow           int
	CompanionMatchStartColumn        int
	CompanionTeleopStartPage         int
	CompanionTeleopStartRow          int
	CompanionTeleopStartColumn       int
	CompanionEndgameStartPage        int
	CompanionEndgameStartRow         int
	CompanionEndgameStartColumn      int
	CompanionMatchEndPage            int
	CompanionMatchEndRow             int
	CompanionMatchEndColumn          int
	CompanionPostResultPage          int
	CompanionPostResultRow           int
	CompanionPostResultColumn        int
	CompanionAllianceSelectionPage   int
	CompanionAllianceSelectionRow    int
	CompanionAllianceSelectionColumn int
	CompanionMatchAbortPage          int
	CompanionMatchAbortRow           int
	CompanionMatchAbortColumn        int
	WarmupDurationSec                int
	AutoDurationSec                  int
	PauseDurationSec                 int
	TeleopDurationSec                int
	WarningRemainingDurationSec      int

	// Hub lighting. Stored here rather than in config.yaml so it survives a power cycle and
	// can be changed from the Settings page without a restart.
	//
	// Two outputs, one at a time. DMX is upstream's: its full-field fixture layout at
	// HubLedsAddress, over E1.31 sACN or, with HubLedsArtNet, Art-Net; a blank address sends
	// nothing. HubRelays replaces it with one relay per HUB on the controller's own GPIO,
	// switching a red or a blue lamp. Relay pins are BCM numbers, and HubRelayActiveLow is
	// for boards that switch on when their input is pulled LOW.
	HubLedsAddress    string
	HubLedsArtNet     bool
	HubRelays         bool
	HubRelayRedPin    int
	HubRelayBluePin   int
	HubRelayActiveLow bool

	// Hub FUEL counter. A webcam watching the field reports the FUEL scored into both
	// HUBs; see docs/spec-hub-fuel-counter-feed.md. A blank address means no counter, and
	// both HUBs then score nothing. Datagrams are accepted from that address only.
	HubCounterAddress string
	HubCounterPort    int

	// AutoWinnerMode is how the AUTO result is decided: "random", "red", "blue", or
	// "counted". Stored rather than held only in arena state because a field that runs
	// Counted runs it every day, and falling back to Random on a power cycle is the kind
	// of error nobody notices until the wrong HUB goes dark mid-match.
	AutoWinnerMode string
}

// applyHubRelayDefaults fills in relay pins that a database predating them has no value
// for, so the Settings page offers working pins rather than zeroes. Only unset pins are
// filled, so a configured field is never overwritten; they persist on the next save.
func (settings *EventSettings) applyHubRelayDefaults() {
	if settings.HubRelayRedPin == 0 {
		settings.HubRelayRedPin = hardware.DefaultHubRelayRedPin
	}
	if settings.HubRelayBluePin == 0 {
		settings.HubRelayBluePin = hardware.DefaultHubRelayBluePin
	}
	// Same reasoning for the counter port: a database predating the counter has no value
	// for it, and a zero would read as "no counter" rather than as "not configured yet".
	// The address is left blank, which is what actually turns the counter off.
	if settings.HubCounterPort == 0 {
		settings.HubCounterPort = hardware.DefaultHubCounterPort
	}
	// A database predating the setting has no mode stored, and an empty string is not one.
	if settings.AutoWinnerMode == "" {
		settings.AutoWinnerMode = DefaultAutoWinnerMode
	}
}

// DefaultAutoWinnerMode is what a field decides the AUTO result by until it says
// otherwise. Random rather than Counted: most practice fields have no camera, and a
// default that blocks match start on hardware nobody has would be a poor one.
const DefaultAutoWinnerMode = "random"

func (database *Database) GetEventSettings() (*EventSettings, error) {
	allEventSettings, err := database.eventSettingsTable.getAll()
	if err != nil {
		return nil, err
	}
	if len(allEventSettings) == 1 {
		settings := &allEventSettings[0]
		settings.applyHubRelayDefaults()
		return settings, nil
	}

	// Database record doesn't exist yet; create it now.
	eventSettings := EventSettings{
		Name:                        "Untitled Event",
		PlayoffType:                 DoubleEliminationPlayoff,
		NumPlayoffAlliances:         8,
		SelectionRound2Order:        "L",
		SelectionRound3Order:        "",
		SelectionShowUnpickedTeams:  true,
		TbaDownloadEnabled:          false,
		ApChannel:                   36,
		CompanionAddress:            "",
		WarmupDurationSec:           game.MatchTiming.WarmupDurationSec,
		AutoDurationSec:             game.MatchTiming.AutoDurationSec,
		PauseDurationSec:            game.MatchTiming.PauseDurationSec,
		TeleopDurationSec:           game.MatchTiming.TeleopDurationSec,
		WarningRemainingDurationSec: game.MatchTiming.WarningRemainingDurationSec,

		HubRelayRedPin:  hardware.DefaultHubRelayRedPin,
		HubRelayBluePin: hardware.DefaultHubRelayBluePin,
		HubCounterPort:  hardware.DefaultHubCounterPort,
		AutoWinnerMode:  DefaultAutoWinnerMode,
	}

	if err := database.eventSettingsTable.create(&eventSettings); err != nil {
		return nil, err
	}
	return &eventSettings, nil
}

func (database *Database) UpdateEventSettings(eventSettings *EventSettings) error {
	return database.eventSettingsTable.update(eventSettings)
}

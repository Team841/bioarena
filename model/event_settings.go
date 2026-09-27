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
}

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
	}

	if err := database.eventSettingsTable.create(&eventSettings); err != nil {
		return nil, err
	}
	return &eventSettings, nil
}

func (database *Database) UpdateEventSettings(eventSettings *EventSettings) error {
	return database.eventSettingsTable.update(eventSettings)
}

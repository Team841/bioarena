// Copyright 2014 Team 254. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)
//
// Model and datastore read/write methods for event-level configuration.

package model

import (
	"github.com/Team254/cheesy-arena/game"
)

type PlayoffType int

const (
	DoubleEliminationPlayoff PlayoffType = iota
	SingleEliminationPlayoff
)

type EventSettings struct {
	Id                               int `db:"id"`
	Name                             string
	PlayoffType                      PlayoffType
	NumPlayoffAlliances              int
	SelectionRound2Order             string
	SelectionRound3Order             string
	SelectionShowUnpickedTeams       bool
	TbaDownloadEnabled               bool
	TbaPublishingEnabled             bool
	TbaEventCode                     string
	TbaSecretId                      string
	TbaSecret                        string
	NexusEnabled                     bool
	NetworkSecurityEnabled           bool
	ApAddress                        string
	ApPassword                       string
	ApChannel                        int
	SwitchAddress                    string
	SwitchPassword                   string
	SwitchDSPortUpCommands           string
	SwitchDSPortDownCommands         string
	RedEStopPanelAddress             string
	BlueEStopPanelAddress            string
	FieldEStopPin                    int
	AdminPassword                    string
	TeamSignRed1Id                   int
	TeamSignRed2Id                   int
	TeamSignRed3Id                   int
	TeamSignRedTimerId               int
	TeamSignBlue1Id                  int
	TeamSignBlue2Id                  int
	TeamSignBlue3Id                  int
	TeamSignBlueTimerId              int
	AutoConfigureTeams               bool
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
}

func (database *Database) GetEventSettings() (*EventSettings, error) {
	allEventSettings, err := database.eventSettingsTable.getAll()
	if err != nil {
		return nil, err
	}
	if len(allEventSettings) == 1 {
		return &allEventSettings[0], nil
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
		AutoConfigureTeams:          true,
		ApChannel:                   36,
		SwitchDSPortUpCommands:      "interface range FastEthernet0/1-6\nno shutdown",
		SwitchDSPortDownCommands:    "interface range FastEthernet0/1-6\nshutdown",
		CompanionAddress:            "",
		WarmupDurationSec:           game.MatchTiming.WarmupDurationSec,
		AutoDurationSec:             game.MatchTiming.AutoDurationSec,
		PauseDurationSec:            game.MatchTiming.PauseDurationSec,
		TeleopDurationSec:           game.MatchTiming.TeleopDurationSec,
		WarningRemainingDurationSec: game.MatchTiming.WarningRemainingDurationSec,
	}

	if err := database.eventSettingsTable.create(&eventSettings); err != nil {
		return nil, err
	}
	return &eventSettings, nil
}

func (database *Database) UpdateEventSettings(eventSettings *EventSettings) error {
	return database.eventSettingsTable.update(eventSettings)
}
